package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (s *Store) lock() (func() error, error) {
	path := filepath.Join(s.dir, "write.lock")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}
	_, writeErr := fmt.Fprintf(f, "git-task write lock\npid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	syncErr := f.Sync()
	info, statErr := f.Stat()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, statErr, closeErr); err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}
	return func() error {
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, current) {
			return fmt.Errorf("блокировка заменена внешним процессом: %s", path)
		}
		return os.Remove(path)
	}, nil
}
