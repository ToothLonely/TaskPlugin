package app

import (
	"context"
	"reflect"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func addFixtureTask(t *testing.T, p *Plans, ctx context.Context, title, description string, pos task.Position) (task.Task, error) {
	t.Helper()
	item, err := p.Add(ctx, title, description, pos)
	if err != nil {
		return item, err
	}
	editPlan(t, p, func(plan *task.Plan) error { testrepo.FixtureIDs(plan); plan.Revision++; return nil })
	s, err := p.store.Load(ctx)
	if err != nil {
		return task.Task{}, err
	}
	for _, candidate := range s.Plan.Tasks {
		if candidate.Number == item.Number {
			return candidate, nil
		}
	}
	return task.Task{}, task.ErrNotFound
}

func sameAttemptBinding(a, b *task.Attempt) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, right := *a, *b
	left.Status, right.Status = "", ""
	return reflect.DeepEqual(left, right)
}

func sameAttemptBindings(a, b []task.Attempt) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameAttemptBinding(&a[i], &b[i]) {
			return false
		}
	}
	return true
}
