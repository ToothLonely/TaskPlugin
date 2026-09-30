package app

import (
	"context"

	"git-task/internal/task"
)

func (p *Plans) changeTask(ctx context.Context, id string, change func(*task.Plan) (bool, error)) (task.Task, bool, error) {
	base, err := p.store.Load(ctx)
	if err != nil {
		return task.Task{}, false, err
	}
	next := base.Plan
	if _, err = change(&next); err != nil {
		return task.Task{}, false, err
	}
	changed, err := p.store.Save(ctx, base, next)
	if err != nil {
		return task.Task{}, false, err
	}
	item, err := next.FindID(id)
	return item, changed, err
}

func (p *Plans) Move(ctx context.Context, id string, pos task.Position) (task.Task, bool, error) {
	return p.changeTask(ctx, id, func(plan *task.Plan) (bool, error) { return plan.Move(id, pos) })
}

func (p *Plans) Pause(ctx context.Context, id string) (task.Task, bool, error) {
	return p.changeTask(ctx, id, func(plan *task.Plan) (bool, error) { return plan.Pause(id) })
}

func (p *Plans) Archive(ctx context.Context, id string) (task.Task, bool, error) {
	return p.changeTask(ctx, id, func(plan *task.Plan) (bool, error) { return plan.Archive(id) })
}
