package app

import (
	"context"

	"git-task/internal/task"
)

func (p *Plans) changeTask(ctx context.Context, id string, change func(*task.Plan, string) (bool, error)) (task.Task, bool, error) {
	base, err := p.store.Load(ctx)
	if err != nil {
		return task.Task{}, false, err
	}
	next := base.Plan
	selected, err := selectTaskID(next, id)
	if err != nil {
		return task.Task{}, false, err
	}
	if _, err = change(&next, selected.ID); err != nil {
		return task.Task{}, false, err
	}
	changed, err := p.save(ctx, base, next)
	if err != nil {
		return task.Task{}, false, err
	}
	item, err := next.FindID(selected.ID)
	return item, changed, err
}

func (p *Plans) Move(ctx context.Context, id string, pos task.Position) (task.Task, bool, error) {
	return p.changeTask(ctx, id, func(plan *task.Plan, id string) (bool, error) {
		pos, err := selectPosition(*plan, pos)
		if err != nil {
			return false, err
		}
		return plan.Move(id, pos)
	})
}

func (p *Plans) Pause(ctx context.Context, id string, attemptIDs ...string) (task.Task, bool, error) {
	return p.changeTask(ctx, id, func(plan *task.Plan, id string) (bool, error) {
		item, err := plan.FindID(id)
		if err != nil {
			return false, err
		}
		a, err := p.selectAttempt(ctx, *plan, item, attemptIDs)
		if err != nil {
			return false, err
		}
		return plan.Pause(id, a.ID)
	})
}

func (p *Plans) Archive(ctx context.Context, id string) (task.Task, bool, error) {
	return p.changeTask(ctx, id, func(plan *task.Plan, id string) (bool, error) { return plan.Archive(id) })
}
