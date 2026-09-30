package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type fileState struct {
	data   []byte
	mode   os.FileMode
	exists bool
}

func inspect(path string, directory bool) error {
	parent := filepath.Dir(path)
	if parent != path {
		if err := inspect(parent, true); err != nil {
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
		return fmt.Errorf("небезопасный путь hooks: %s", path)
	}
	return nil
}

func read(path string) (fileState, error) {
	if err := inspect(path, false); err != nil {
		return fileState{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	data, err := os.ReadFile(path)
	return fileState{data: data, mode: info.Mode().Perm(), exists: true}, err
}

func equal(a, b fileState) bool {
	return a.exists == b.exists && (!a.exists || a.mode == b.mode && bytes.Equal(a.data, b.data))
}

func create(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Sync(), f.Close())
}

func replace(path string, before, after fileState) (err error) {
	if err := inspect(path, false); err != nil {
		return err
	}
	var tmp string
	if after.exists {
		f, err := os.CreateTemp(filepath.Dir(path), ".git-task-pending-")
		if err != nil {
			return err
		}
		tmp = f.Name()
		defer func() {
			if tmp != "" {
				err = errors.Join(err, os.Remove(tmp))
			}
		}()
		_, writeErr := f.Write(after.data)
		if err := errors.Join(writeErr, f.Chmod(after.mode), f.Sync(), f.Close()); err != nil {
			return err
		}
	}
	current, err := read(path)
	if err != nil {
		return err
	}
	if !equal(current, before) {
		return fmt.Errorf("hook изменён внешним процессом: %s", path)
	}
	if !after.exists {
		if !current.exists {
			return nil
		}
		return os.Remove(path)
	}
	if !current.exists {
		return os.Link(tmp, path)
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	tmp = ""
	return nil
}
