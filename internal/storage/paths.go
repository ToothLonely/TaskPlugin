package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inspectPath rejects symlinks (including Windows junctions reported by Lstat)
// in every existing component before accessing data through that path.
func inspectPath(path string, directory bool) error {
	parent := filepath.Dir(path)
	if parent != path {
		if err := inspectPath(parent, true); err != nil {
			return err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return fmt.Errorf("небезопасный служебный путь: %s", path)
	}
	return nil
}

func readFile(path string) ([]byte, error) {
	if err := inspectPath(path, false); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *Store) inspect(holdingLock bool) error {
	if err := inspectPath(s.dir, true); err != nil {
		return err
	}
	// A live writer legitimately has pending files. Report its lock before
	// interpreting those files as evidence of an interrupted transaction.
	if !holdingLock {
		if _, err := os.Lstat(filepath.Join(s.dir, "write.lock")); err == nil {
			return ErrLocked
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	hasPlan := false
	for _, entry := range entries {
		name := entry.Name()
		if err := inspectPath(filepath.Join(s.dir, name), false); err != nil {
			return err
		}
		switch name {
		case "plan.json":
			hasPlan = true
		case "plan.backup.json", "plan.template.json":
			data, err := readFile(filepath.Join(s.dir, name))
			if err != nil {
				return err
			}
			if err := validateBackup(name, data); err != nil {
				return fmt.Errorf("резервная копия требует разбора: %w", err)
			}
		case "write.lock":
			if !holdingLock {
				return ErrLocked
			}
		case "recovery.lock":
			if !holdingLock {
				return ErrLocked
			}
		case "operation.json":
			// Read-only access remains available for diagnosis. Writers reject
			// the journal unless they explicitly enter operation recovery.
		default:
			if strings.HasPrefix(name, "conflict-") && strings.HasSuffix(name, ".json") {
				continue
			}
			if strings.HasPrefix(name, "pending-") {
				return fmt.Errorf("%w: сохранён файл %s; требуется разбор прерванной записи", ErrInterrupted, filepath.Join(s.dir, name))
			}
			return fmt.Errorf("чужой файл в .git-task: %s", name)
		}
	}
	if !hasPlan && len(entries) > 0 && !(holdingLock && len(entries) == 1 && entries[0].Name() == "write.lock") {
		return fmt.Errorf("%w: отсутствует plan.json; сохранены служебные данные", ErrInterrupted)
	}
	return nil
}
