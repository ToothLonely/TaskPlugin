package app

import (
	"context"
	"errors"
	"fmt"
	"git-task/internal/git"
	"git-task/internal/storage"
)

type RepairPreview struct {
	Storage *storage.Repair
	head    git.Head
}

func (p *Plans) PrepareRepair(ctx context.Context, action string) (*RepairPreview, error) {
	r, err := p.store.PrepareRepair(ctx, action)
	if err != nil {
		return nil, err
	}
	if r.Plan != nil && r.Plan.Team != nil {
		if err := checkQueue(*r.Plan); err != nil {
			return nil, fmt.Errorf("backup содержит несовместимую очередь; исходники сохранены: %w", err)
		}
	}
	preview := &RepairPreview{Storage: r}
	if action == "recover-start" && !r.NoChange {
		preview.head, err = p.git.HeadState(ctx)
		if err != nil {
			return nil, err
		}
		r.Description += fmt.Sprintf("; HEAD=%s %s", preview.head.Ref, preview.head.Commit)
	}
	return preview, nil
}

func (p *Plans) ApplyRepair(ctx context.Context, preview *RepairPreview) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if preview == nil || preview.Storage == nil {
		return "", fmt.Errorf("нет предпросмотра repair")
	}
	if preview.Storage.Action == "recover-start" {
		if preview.Storage.NoChange {
			return "", p.store.CheckRepair(preview.Storage, false)
		}
		head, err := p.git.HeadState(ctx)
		if err != nil {
			return "", err
		}
		if head != preview.head {
			return "", storage.ErrConflict
		}
		_, err = p.recoverStart(ctx, preview.Storage)
		return "", err
	}
	path, err := p.store.ApplyRepair(ctx, preview.Storage)
	if err != nil && path != "" {
		err = errors.Join(err, fmt.Errorf("оригинал сохранён: %s", path))
	}
	return path, err
}
