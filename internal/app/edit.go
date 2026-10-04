package app

import (
	"context"
	"fmt"

	"git-task/internal/storage"
	"git-task/internal/task"
)

type EditPreview struct {
	Task task.Task
	Dir  string
	base storage.Snapshot
	id   string
}

func (p *Plans) PrepareEdit(ctx context.Context, id string) (*EditPreview, error) {
	base, err := p.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if base.PendingOperation {
		return nil, storage.ErrOperation
	}
	item, err := base.Plan.FindID(id)
	if err != nil {
		return nil, err
	}
	return &EditPreview{Task: item, Dir: p.git.Dir, base: base, id: id}, nil
}

func (p *Plans) ApplyEdit(ctx context.Context, preview *EditPreview, opts task.EditOptions) (task.Task, bool, error) {
	if preview == nil {
		return task.Task{}, false, fmt.Errorf("отсутствует подготовленное редактирование")
	}
	next := preview.base.Plan
	if _, err := next.Edit(preview.id, opts); err != nil {
		return task.Task{}, false, err
	}
	changed, err := p.save(ctx, preview.base, next)
	if err != nil {
		return task.Task{}, false, err
	}
	item, err := next.FindID(preview.id)
	return item, changed, err
}

func (p *Plans) Editor(ctx context.Context) (string, error) {
	return p.git.Editor(ctx)
}
