package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) exclude(ctx context.Context) error {
	original, err := readFile(s.excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	present := false
	for _, line := range bytes.Split(original, []byte("\n")) {
		if string(bytes.TrimSuffix(line, []byte("\r"))) == "/.git-task/" {
			present = true
		}
	}
	if !present {
		if err := os.MkdirAll(filepath.Dir(s.excludePath), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(s.excludePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		suffix := []byte("/.git-task/\n")
		if len(original) > 0 && original[len(original)-1] != '\n' {
			suffix = append([]byte("\n"), suffix...)
		}
		_, writeErr := f.Write(suffix)
		err = errors.Join(writeErr, f.Sync(), f.Close())
		if err != nil {
			return fmt.Errorf("запись info/exclude: %w", err)
		}
	}
	ignored, err := s.guard.StorageIgnored(ctx)
	if err != nil {
		return err
	}
	if !ignored {
		return fmt.Errorf("правила Git переопределяют /.git-task/; исправьте конфликт исключений")
	}
	return nil
}
