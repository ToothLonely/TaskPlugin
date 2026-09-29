package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"git-task/internal/storage"
	"git-task/internal/transfer"
)

func (p *Plans) Export(ctx context.Context, format string) ([]byte, error) {
	snapshot, err := p.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot.PendingOperation {
		return nil, storage.ErrOperation
	}
	switch format {
	case "markdown":
		return transfer.EncodeMarkdown(snapshot.Plan)
	case "json":
		return transfer.EncodeJSON(snapshot.Plan)
	default:
		return nil, fmt.Errorf("неизвестный формат %q", format)
	}
}

func (p *Plans) ExportFile(ctx context.Context, path string, data []byte) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	path = filepath.Join(parent, filepath.Base(path))
	repo, err := p.git.Discover(ctx)
	if err != nil {
		return err
	}
	for _, protected := range []string{filepath.Join(repo.Root, ".git-task"), repo.GitDir, repo.CommonDir} {
		resolved, err := filepath.EvalSymlinks(protected)
		if err != nil {
			return err
		}
		protected, err := outputWithin(resolved, parent)
		if err != nil {
			return err
		}
		if protected {
			return fmt.Errorf("экспорт в служебный каталог запрещён: %s", path)
		}
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("путь экспорта уже существует: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return publishExport(ctx, path, data)
}

func outputWithin(dir, parent string) (bool, error) {
	protected, err := os.Stat(dir)
	if err != nil {
		return false, err
	}
	for {
		info, err := os.Stat(parent)
		if err != nil {
			return false, err
		}
		if os.SameFile(protected, info) {
			return true, nil
		}
		next := filepath.Dir(parent)
		if parent == next {
			return false, nil
		}
		parent = next
	}
}

func publishExport(ctx context.Context, path string, data []byte) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".git-task-export-*")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.Remove(f.Name())) }()
	_, writeErr := f.Write(data)
	if err = errors.Join(writeErr, f.Sync(), f.Close(), ctx.Err()); err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return fmt.Errorf("публикация экспорта без перезаписи: %w", err)
	}
	return nil
}
