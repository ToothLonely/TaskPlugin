package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"git-task/internal/task"
)

func (s *Store) SaveShared(ctx context.Context, base Snapshot, next task.Plan) (changed bool, err error) {
	data, err := encode(next)
	if err != nil {
		return false, err
	}
	if next.Revision <= base.Plan.Revision {
		return false, fmt.Errorf("общая запись требует увеличения revision")
	}
	if base.dir != s.dir || len(base.raw) == 0 {
		return false, ErrConflict
	}
	if err = s.check(ctx, false); err != nil {
		return false, err
	}
	unlock, err := s.lock()
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if err = s.check(ctx, true); err != nil {
		return false, err
	}
	if err = s.noOperation(); err != nil {
		return false, err
	}
	if err = s.compare(base.raw); err != nil {
		return false, s.preserveConflict(data, err)
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

func (s *Store) PreserveShared(ctx context.Context, data []byte, cause error) (result error) {
	if err := s.check(ctx, false); err != nil {
		return errors.Join(cause, err)
	}
	unlock, err := s.lock()
	if err != nil {
		return errors.Join(cause, err)
	}
	defer func() { result = errors.Join(result, unlock()) }()
	digest := sha256.Sum256(data)
	path := filepath.Join(s.dir, fmt.Sprintf("conflict-%x.json", digest))
	if err := inspectPath(path, false); err != nil {
		return errors.Join(cause, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			old, readErr := readFile(path)
			if readErr != nil || !bytes.Equal(old, data) {
				return errors.Join(cause, ErrConflict, readErr)
			}
			return fmt.Errorf("полученная версия сохранена в %s: %w", path, cause)
		}
		return errors.Join(cause, err)
	}
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	return fmt.Errorf("полученная версия сохранена в %s: %w", f.Name(), errors.Join(cause, err))
}
