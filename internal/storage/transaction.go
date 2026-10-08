package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// writePending leaves a durable candidate on any failure after creation. It
// must not silently delete evidence needed to diagnose an interrupted write.
func (s *Store) writePending(data []byte) (string, error) {
	f, err := os.CreateTemp(s.dir, "pending-*.json")
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return f.Name(), fmt.Errorf("подготовка %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}

func (s *Store) preserveConflict(data []byte, cause error) error {
	path, err := s.writePending(data)
	return fmt.Errorf("подготовленный результат: %s: %w", path, errors.Join(cause, err))
}

func (s *Store) point(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.checkpoint != nil {
		return s.checkpoint(name)
	}
	return nil
}

func (s *Store) install(ctx context.Context, original, next []byte) error {
	return s.installWithBackup(ctx, original, next, "plan.backup.json")
}

func (s *Store) installWithBackup(ctx context.Context, original, next []byte, backupName string) error {
	candidate, err := s.writePending(next)
	if err != nil {
		return err
	}
	if err = s.point(ctx, "candidate"); err != nil {
		return fmt.Errorf("подготовлен %s: %w", candidate, err)
	}
	if err = s.compare(original); err != nil {
		return fmt.Errorf("сохранён %s: %w", candidate, err)
	}
	if original != nil {
		backup, err := s.writePending(original)
		if err != nil {
			return err
		}
		backupPath := filepath.Join(s.dir, backupName)
		if err = inspectPath(backupPath, false); err != nil {
			return err
		}
		if previous, readErr := readFile(backupPath); readErr == nil {
			if err = validateBackup(backupName, previous); err != nil {
				return fmt.Errorf("повреждённый backup сохранён: %w", err)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if err = os.Rename(backup, backupPath); err != nil {
			return fmt.Errorf("сохранение backup: %w", err)
		}
	}
	if err = s.point(ctx, "backup"); err != nil {
		return fmt.Errorf("сохранён %s: %w", candidate, err)
	}
	if err = s.compare(original); err != nil {
		return fmt.Errorf("сохранён %s: %w", candidate, err)
	}
	planPath := filepath.Join(s.dir, "plan.json")
	if original == nil {
		// Link publishes complete bytes and refuses an existing destination.
		if err = os.Link(candidate, planPath); err != nil {
			return fmt.Errorf("создание plan.json; сохранён %s: %w", candidate, err)
		}
		if err = os.Remove(candidate); err != nil {
			return fmt.Errorf("план создан; не удалён временный файл: %w", err)
		}
	} else if err = os.Rename(candidate, planPath); err != nil {
		return fmt.Errorf("замена plan.json; сохранён %s: %w", candidate, err)
	}
	if err = s.point(ctx, "installed"); err != nil {
		return fmt.Errorf("план уже сохранён; проверьте состояние перед повтором: %w", err)
	}
	return nil
}
