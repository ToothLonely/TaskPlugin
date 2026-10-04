package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"git-task/internal/task"
)

// ErrOperation requires explicit recovery, never an automatic repeat of Git.
var ErrOperation = errors.New("незавершённая операция: сохранён operation.json; требуется явное восстановление")

func (s *Store) noOperation() error {
	_, err := os.Lstat(filepath.Join(s.dir, "operation.json"))
	if err == nil {
		return ErrOperation
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Operation holds the common storage lock across a local Git operation.
// It is valid only inside WithOperation; callbacks must not reenter Store.
type Operation struct{ store *Store }

// Journal preserves exact bytes, including whitespace in an externally edited
// source plan. The digest detects corruption; it is not an authentication key.
type Journal struct {
	Version  int             `json:"version"`
	Original []byte          `json:"original"`
	Result   []byte          `json:"result"`
	Intent   json.RawMessage `json:"intent"`
	Digest   string          `json:"digest"`
	raw      []byte
}

func (j Journal) checksum() string {
	j.Digest = ""
	data, _ := json.Marshal(j)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// WithOperation serializes start, attach and recovery with ordinary plan writes.
func (s *Store) WithOperation(ctx context.Context, run func(*Operation) error) (err error) {
	if err = s.check(ctx, false); err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if err = s.check(ctx, true); err != nil {
		return err
	}
	return run(&Operation{store: s})
}

// Load reads the plan while holding the operation lock.
func (o *Operation) Load() (Snapshot, error) { return o.store.load() }

// Journal returns nil when no operation needs recovery.
func (o *Operation) Journal() (*Journal, error) {
	data, err := readFile(filepath.Join(o.store.dir, "operation.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeJournal(data)
}

func decodeJournal(data []byte) (*Journal, error) {
	var j Journal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&j); err != nil {
		return nil, fmt.Errorf("повреждён operation.json: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("повреждён operation.json: лишние данные")
	}
	if j.Version != 1 || j.Digest != j.checksum() {
		return nil, fmt.Errorf("повреждён operation.json: версия или контрольная сумма")
	}
	if _, err := decode(j.Original); err != nil {
		return nil, err
	}
	if _, err := decode(j.Result); err != nil {
		return nil, err
	}
	j.raw = data
	return &j, nil
}

// Prepare durably records both sides before any external effect.
func (o *Operation) Prepare(ctx context.Context, base Snapshot, next task.Plan, intent any) (*Journal, error) {
	if err := o.store.noOperation(); err != nil {
		return nil, err
	}
	if base.dir != o.store.dir || len(base.raw) == 0 {
		return nil, ErrConflict
	}
	if err := o.store.compare(base.raw); err != nil {
		return nil, err
	}
	original, err := decode(base.raw)
	if err != nil {
		return nil, err
	}
	next, err = task.Queue(original, next)
	if err != nil {
		return nil, err
	}
	result, err := encode(next)
	if err != nil {
		return nil, err
	}
	if next.Revision <= original.Revision {
		return nil, fmt.Errorf("операция требует увеличения revision")
	}
	ignored, err := o.store.guard.StorageIgnored(ctx)
	if err != nil {
		return nil, err
	}
	if !ignored {
		return nil, fmt.Errorf(".git-task не исключён из Git")
	}
	payload, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	j := &Journal{Version: 1, Original: base.raw, Result: result, Intent: payload}
	j.Digest = j.checksum()
	j.raw, err = json.MarshalIndent(j, "", "  ")
	if err != nil {
		return nil, err
	}
	candidate, err := o.store.writePending(j.raw)
	if err != nil {
		return nil, err
	}
	if err = os.Link(candidate, filepath.Join(o.store.dir, "operation.json")); err != nil {
		return nil, err
	}
	if err = os.Remove(candidate); err != nil {
		return nil, err
	}
	return j, nil
}

func (o *Operation) verify(j *Journal) error {
	current, err := readFile(filepath.Join(o.store.dir, "operation.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(current, j.raw) {
		return fmt.Errorf("журнал изменён: %w", ErrConflict)
	}
	return nil
}

// State distinguishes the source from this operation's exact installed result.
func (o *Operation) State(j *Journal) (installed bool, err error) {
	if err = o.verify(j); err != nil {
		return false, err
	}
	current, err := readFile(filepath.Join(o.store.dir, "plan.json"))
	if err != nil {
		return false, err
	}
	if bytes.Equal(current, j.Result) {
		return true, nil
	}
	if bytes.Equal(current, j.Original) {
		return false, nil
	}
	return false, ErrConflict
}

// Commit never rewrites an already installed result or its backup.
func (o *Operation) Commit(ctx context.Context, j *Journal) error {
	installed, err := o.State(j)
	if err != nil || installed {
		return err
	}
	if err = o.store.check(ctx, true); err != nil {
		return err
	}
	return o.store.install(ctx, j.Original, j.Result)
}

func (o *Operation) CommitWithAction(ctx context.Context, j *Journal) (string, error) {
	before, err := decode(j.Original)
	if err != nil {
		return "", err
	}
	after, err := decode(j.Result)
	if err != nil {
		return "", err
	}
	if err := o.Commit(ctx, j); err != nil {
		return "", err
	}
	return queuedActionID(before, after), nil
}

// Close removes only the unchanged journal, after the caller verifies Git facts.
func (o *Operation) Close(j *Journal) error {
	if _, err := o.State(j); err != nil {
		return err
	}
	return os.Remove(filepath.Join(o.store.dir, "operation.json"))
}
