package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"git-task/internal/task"
)

var ErrMigration = errors.New("схема 1 требует явной миграции: git task migrate")

func schemaVersion(data []byte) int {
	var header struct {
		Version int `json:"schema_version"`
	}
	if json.Unmarshal(data, &header) != nil {
		return 0
	}
	return header.Version
}

func (s *Store) Migrate(ctx context.Context) (changed bool, err error) {
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
	data, err := readFile(filepath.Join(s.dir, "plan.json"))
	if err != nil {
		return false, err
	}
	if schemaVersion(data) == task.SchemaVersion {
		_, err := decode(data)
		return false, err
	}
	next, err := task.MigrateV1(data)
	if err != nil {
		return false, err
	}
	path := filepath.Join(s.dir, "plan.schema-1.json")
	if err = inspectPath(path, false); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		previous, readErr := readFile(path)
		if readErr != nil || !bytes.Equal(previous, data) {
			return false, fmt.Errorf("исходник миграции отличается: %w", errors.Join(ErrConflict, readErr))
		}
	} else {
		_, writeErr := f.Write(data)
		if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
			return false, err
		}
	}
	encoded, err := encode(next)
	if err != nil {
		return false, err
	}
	if err = s.install(ctx, data, encoded); err != nil {
		return false, err
	}
	return true, nil
}
