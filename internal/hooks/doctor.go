package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (i Installer) Diagnose(ctx context.Context) ([]string, error) {
	dir, err := i.Git.HooksDirectory(ctx)
	if err != nil {
		return nil, err
	}
	if err := inspect(dir, true); err != nil {
		return nil, err
	}
	entries, err := prepare(dir, "", false)
	if err != nil {
		return nil, err
	}
	var result []string
	if _, err := os.Lstat(filepath.Join(dir, ".git-task-hooks.lock")); err == nil {
		return nil, fmt.Errorf("lock hooks требует ручного разбора: %s", dir)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if !e.stored.exists {
			continue
		}
		if !equal(e.current, e.record.installed()) {
			return nil, fmt.Errorf("установка hook %s прервана; повторите hooks install", e.record.Event)
		}
		if err := inspect(e.record.Binary, false); err != nil {
			return nil, err
		}
		if _, err := os.Stat(e.record.Binary); err != nil {
			return nil, fmt.Errorf("бинарник hook отсутствует: %s: %w", e.record.Binary, err)
		}
		result = append(result, e.record.Event)
	}
	for _, event := range events {
		current, err := read(filepath.Join(dir, event))
		if err != nil {
			return result, err
		}
		stored, err := read(filepath.Join(dir, ".git-task-"+event+".json"))
		if err != nil {
			return result, err
		}
		if !stored.exists && current.exists {
			if !supported(current) {
				return result, fmt.Errorf("hook %s требует ручного подключения по README.md", event)
			}
			result = append(result, event+" (чужой обработчик сохранён; git-task не подключён)")
		}
	}
	return result, nil
}
