// Package storage persists a single validated local plan without changing Git history.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"git-task/internal/task"
)

var (
	ErrLocked      = errors.New("хранилище заблокировано: write.lock; проверьте владельца, не снимайте блокировку по возрасту")
	ErrConflict    = errors.New("план изменён после чтения; сравните сохранённый результат с текущими данными")
	ErrInterrupted = errors.New("незавершённая запись хранилища")
)

// Guard provides the repository checks needed by storage, without exposing Git commands.
type Guard interface {
	CheckStorage(context.Context) error
	StorageIgnored(context.Context) (bool, error)
}

// Store is immutable after construction. Concurrent writers coordinate through
// an exclusive filesystem lock, not an in-process mutex.
type Store struct {
	dir         string
	excludePath string
	guard       Guard
	// checkpoint is an instance-local fault injection boundary used by tests.
	checkpoint func(string) error
}

// New receives absolute paths resolved by the repository adapter.
func New(root, excludePath string, guard Guard) *Store {
	return &Store{dir: filepath.Join(root, ".git-task"), excludePath: excludePath, guard: guard}
}

// Snapshot retains the exact source bytes independently of the editable Plan.
type Snapshot struct {
	Plan             task.Plan
	PendingOperation bool
	raw              []byte
	dir              string
}

func (s Snapshot) SameVersion(other Snapshot) bool {
	return s.dir == other.dir && bytes.Equal(s.raw, other.raw)
}

func (s *Store) check(ctx context.Context, holdingLock bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.guard.CheckStorage(ctx); err != nil {
		return err
	}
	return s.inspect(holdingLock)
}

// Load never repairs damaged files or substitutes a backup.
func (s *Store) Load(ctx context.Context) (Snapshot, error) {
	if err := s.check(ctx, false); err != nil {
		return Snapshot{}, err
	}
	return s.load()
}

func (s *Store) load() (Snapshot, error) {
	data, err := readFile(filepath.Join(s.dir, "plan.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, fmt.Errorf("план отсутствует; выполните git task init: %w", err)
	}
	if err != nil {
		return Snapshot{}, err
	}
	plan, err := decode(data)
	if err != nil {
		return Snapshot{}, err
	}
	_, journalErr := os.Lstat(filepath.Join(s.dir, "operation.json"))
	if journalErr != nil && !errors.Is(journalErr, os.ErrNotExist) {
		return Snapshot{}, journalErr
	}
	return Snapshot{Plan: plan, PendingOperation: journalErr == nil, raw: data, dir: s.dir}, nil
}

// Init creates the plan only at a free, safe path. Repeating it preserves bytes.
func (s *Store) Init(ctx context.Context, target string) (created bool, err error) {
	plan, err := task.NewPlan(target)
	if err != nil {
		return false, err
	}
	if err = s.check(ctx, false); err != nil {
		return false, err
	}
	if err = inspectPath(s.excludePath, false); err != nil {
		return false, err
	}
	// Validate an existing plan before touching even the exclusion file.
	previous, err := s.load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil && previous.Plan.TargetBranch != target {
		return false, fmt.Errorf("план уже использует target %q", previous.Plan.TargetBranch)
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return false, err
	}
	unlock, err := s.lock()
	if err != nil {
		return false, err
	}
	defer func() {
		if releaseErr := unlock(); releaseErr != nil {
			if created {
				releaseErr = fmt.Errorf("план уже создан; ошибка снятия блокировки: %w", releaseErr)
			}
			err = errors.Join(err, releaseErr)
		}
	}()
	if err = s.check(ctx, true); err != nil {
		return false, err
	}
	if err = s.noOperation(); err != nil {
		return false, err
	}
	previous, err = s.load()
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if exists && previous.Plan.TargetBranch != target {
		return false, fmt.Errorf("план уже использует target %q", previous.Plan.TargetBranch)
	}
	if err = s.exclude(ctx); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	data, err := encode(plan)
	if err != nil {
		return false, err
	}
	if err = s.install(ctx, nil, data); err != nil {
		return false, err
	}
	return true, nil
}

// Save commits a validated result only against its exact source snapshot.
// It returns false for a semantic no-op without rewriting plan or backup.
func (s *Store) Save(ctx context.Context, base Snapshot, next task.Plan) (changed bool, err error) {
	return s.save(ctx, base, next, false)
}

func (s *Store) Import(ctx context.Context, base Snapshot, next task.Plan) (bool, error) {
	return s.save(ctx, base, next, true)
}

func (s *Store) save(ctx context.Context, base Snapshot, next task.Plan, importing bool) (changed bool, err error) {
	data, err := encode(next)
	if err != nil {
		return false, err
	}
	if base.dir != s.dir || len(base.raw) == 0 {
		return false, fmt.Errorf("снимок не принадлежит этому хранилищу")
	}
	original, err := decode(base.raw)
	if err != nil {
		return false, err
	}
	canonical, err := encode(original)
	if err != nil {
		return false, err
	}
	if err = s.check(ctx, false); err != nil {
		return false, err
	}
	unlock, err := s.lock()
	if err != nil {
		return false, err
	}
	defer func() {
		if releaseErr := unlock(); releaseErr != nil {
			if changed {
				releaseErr = fmt.Errorf("план уже сохранён; ошибка снятия блокировки: %w", releaseErr)
			}
			err = errors.Join(err, releaseErr)
		}
	}()
	if err = s.check(ctx, true); err != nil {
		return false, err
	}
	if err = s.noOperation(); err != nil {
		return false, err
	}
	if err = s.compare(base.raw); err != nil {
		return false, s.preserveConflict(data, err)
	}
	if importing && len(original.Tasks) != 0 {
		return false, fmt.Errorf("импорт требует пустой план")
	}
	if bytes.Equal(canonical, data) {
		return false, nil
	}
	if !importing && next.Revision <= original.Revision {
		return false, fmt.Errorf("изменение требует увеличения revision")
	}
	ignored, err := s.guard.StorageIgnored(ctx)
	if err != nil {
		return false, err
	}
	if !ignored {
		return false, fmt.Errorf(".git-task не исключён из Git; выполните init с прежним target")
	}
	if err = s.install(ctx, base.raw, data); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) compare(expected []byte) error {
	actual, err := readFile(filepath.Join(s.dir, "plan.json"))
	if expected == nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Join(ErrConflict, err)
	}
	if !bytes.Equal(actual, expected) {
		return ErrConflict
	}
	return nil
}
