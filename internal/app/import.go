package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/transfer"
)

type ImportPreview struct {
	Plan        task.Plan
	Destination string
	base        storage.Snapshot
	data        []byte
}

func (p *Plans) PrepareImport(ctx context.Context, path, format string) (*ImportPreview, error) {
	base, err := p.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if base.PendingOperation {
		return nil, storage.ErrOperation
	}
	if len(base.Plan.Tasks) != 0 {
		return nil, fmt.Errorf("импорт требует пустой план; существующие задачи сохранены")
	}
	data, err := readImportFile(path)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var next task.Plan
	switch format {
	case "markdown":
		next, err = transfer.DecodeMarkdown(data, base.Plan.TargetBranch)
	case "json":
		next, err = transfer.DecodeJSON(data)
	default:
		return nil, fmt.Errorf("неизвестный формат %q", format)
	}
	if err != nil {
		return nil, fmt.Errorf("импорт %s: %w", path, err)
	}
	canonical, err := transfer.EncodeJSON(next)
	if err != nil {
		return nil, err
	}
	return &ImportPreview{Plan: next, Destination: filepath.Join(p.git.Dir, ".git-task", "plan.json"), base: base, data: canonical}, nil
}

func readImportFile(path string) (data []byte, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("источник импорта должен быть обычным файлом: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("чтение импорта: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("источник импорта должен быть обычным файлом: %s", path)
	}
	return io.ReadAll(f)
}

func (p *Plans) ApplyImport(ctx context.Context, preview *ImportPreview) error {
	if preview == nil {
		return fmt.Errorf("отсутствует подготовленный импорт")
	}
	current, err := p.store.Load(ctx)
	if err != nil {
		return err
	}
	if !preview.base.SameVersion(current) {
		return storage.ErrConflict
	}
	if current.PendingOperation {
		return storage.ErrOperation
	}
	if len(current.Plan.Tasks) != 0 {
		return fmt.Errorf("импорт требует пустой план")
	}
	next, err := transfer.DecodeJSON(preview.data)
	if err != nil {
		return err
	}
	_, id, err := p.store.ImportWithAction(ctx, preview.base, next)
	p.recordAction(id)
	return err
}
