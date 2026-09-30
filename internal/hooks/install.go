package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"git-task/internal/git"
)

var events = [...]string{"post-commit", "post-merge", "post-checkout", "post-rewrite"}

type Installer struct {
	Git    *git.Client
	Binary string
}

type record struct {
	Version        int         `json:"version"`
	Event          string      `json:"event"`
	Binary         string      `json:"binary"`
	Original       []byte      `json:"original"`
	OriginalExists bool        `json:"original_exists"`
	Mode           os.FileMode `json:"mode"`
	Wrapper        []byte      `json:"wrapper"`
}

func (r record) original() fileState {
	return fileState{data: r.Original, mode: r.Mode, exists: r.OriginalExists}
}

func (r record) installed() fileState {
	mode := r.Mode | 0700
	if runtime.GOOS == "windows" {
		mode = 0666
	}
	return fileState{data: r.Wrapper, mode: mode, exists: true}
}

func enabled(s fileState) bool {
	return s.exists && (runtime.GOOS == "windows" || s.mode&0111 != 0)
}

func supported(s fileState) bool {
	if !enabled(s) {
		return true
	}
	first, _, ok := strings.Cut(string(s.data), "\n")
	if !ok || first != "#!/bin/sh" || bytes.Contains(s.data, []byte{'\r'}) || bytes.Contains(s.data, []byte{0}) {
		return false
	}
	lower := strings.ToLower(string(s.data))
	for _, manager := range []string{"husky", "lefthook", "pre-commit", "overcommit", "git-task", "git_task_"} {
		if strings.Contains(lower, manager) {
			return false
		}
	}
	return true
}

type entry struct {
	path    string
	journal string
	current fileState
	stored  fileState
	record  record
}

func prepare(dir, binary string, install bool) ([]entry, error) {
	files, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".git-task-pending-") {
			return nil, fmt.Errorf("прерванная запись hooks требует ручного разбора: %s", filepath.Join(dir, file.Name()))
		}
	}
	var entries []entry
	for _, event := range events {
		e := entry{path: filepath.Join(dir, event), journal: filepath.Join(dir, ".git-task-"+event+".json")}
		var err error
		if e.current, err = read(e.path); err != nil {
			return nil, err
		}
		if e.stored, err = read(e.journal); err != nil {
			return nil, err
		}
		if e.stored.exists {
			decoder := json.NewDecoder(bytes.NewReader(e.stored.data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&e.record); err != nil {
				return nil, fmt.Errorf("журнал hooks требует разбора: %s: %w", e.journal, err)
			}
			var extra any
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("лишние данные журнала hooks: %s", e.journal)
			}
			r := e.record
			if r.Version != 1 || r.Event != event || !filepath.IsAbs(r.Binary) || strings.ContainsAny(r.Binary, "\x00\r\n") || r.Mode & ^os.FileMode(0777) != 0 || !supported(r.original()) || !bytes.Equal(r.Wrapper, wrapper(event, r.Binary, r.Original, enabled(r.original()))) {
				return nil, fmt.Errorf("неизвестный или изменённый журнал hooks: %s", e.journal)
			}
			if !equal(e.current, r.installed()) && !equal(e.current, r.original()) {
				return nil, fmt.Errorf("hook изменён после установки; сохранён без изменений: %s; используйте ручной разбор из docs/HOOKS.md", e.path)
			}
			if install && binary != r.Binary {
				return nil, fmt.Errorf("hooks привязаны к другому бинарнику; сначала выполните hooks uninstall")
			}
		} else {
			if !install {
				continue
			}
			if !supported(e.current) {
				return nil, fmt.Errorf("hook или сторонний менеджер не поддерживает автоматическую цепочку: %s; используйте ручное подключение из docs/HOOKS.md", e.path)
			}
			e.record = record{Version: 1, Event: event, Binary: binary, Original: e.current.data, OriginalExists: e.current.exists, Mode: e.current.mode}
			e.record.Wrapper = wrapper(event, binary, e.current.data, enabled(e.current))
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (i Installer) Install(ctx context.Context) (bool, error) {
	binary, err := filepath.Abs(i.Binary)
	if err != nil || i.Binary == "" || strings.ContainsAny(binary, "\x00\r\n") {
		return false, fmt.Errorf("неверный путь бинарника hooks")
	}
	info, err := os.Stat(binary)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("бинарник hooks не является обычным файлом")
	}
	return i.change(ctx, binary, true)
}

func (i Installer) Uninstall(ctx context.Context) (bool, error) {
	return i.change(ctx, "", false)
}

func (i Installer) change(ctx context.Context, binary string, install bool) (changed bool, err error) {
	dir, err := i.Git.HooksDirectory(ctx)
	if err != nil {
		return false, err
	}
	if err := inspect(dir, true); err != nil {
		return false, err
	}
	if _, err := prepare(dir, binary, install); err != nil {
		return false, err
	}
	if !install {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	} else if err := os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	entries, err := prepare(dir, binary, install)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		if e.stored.exists {
			current, err := read(e.journal)
			if err != nil {
				return changed, err
			}
			if !equal(current, e.stored) {
				return changed, fmt.Errorf("журнал hooks изменён внешним процессом: %s", e.journal)
			}
		}
		if install {
			if !e.stored.exists {
				data, err := json.Marshal(e.record)
				if err != nil {
					return changed, err
				}
				if err := create(e.journal, data, 0600); err != nil {
					return changed, err
				}
				changed = true
			}
			if equal(e.current, e.record.installed()) {
				continue
			}
			if err := replace(e.path, e.current, e.record.installed()); err != nil {
				return changed, err
			}
			changed = true
		} else {
			if !equal(e.current, e.record.original()) {
				if err := replace(e.path, e.current, e.record.original()); err != nil {
					return changed, err
				}
				changed = true
			}
			if err := replace(e.journal, e.stored, fileState{}); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	return changed, nil
}

func lock(dir string) (func() error, error) {
	path := filepath.Join(dir, ".git-task-hooks.lock")
	if err := create(path, []byte(fmt.Sprintf("git-task hooks lock\npid=%d\n", os.Getpid())), 0600); err != nil {
		return nil, fmt.Errorf("блокировка hooks недоступна; прерванную установку разберите вручную: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	return func() error {
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, current) {
			return fmt.Errorf("блокировка hooks заменена внешним процессом")
		}
		return os.Remove(path)
	}, nil
}
