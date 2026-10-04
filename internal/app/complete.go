package app

import (
	"context"
	"fmt"
	"time"

	"git-task/internal/storage"
	"git-task/internal/task"
)

type CompletionPreview struct {
	Task      task.Task
	Commit    string
	NoChange  bool
	base      storage.Snapshot
	id        string
	commit    string
	target    string
	attemptID string
}

func (p *Plans) PrepareComplete(ctx context.Context, id, commit string, attemptIDs ...string) (*CompletionPreview, error) {
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
	if item.Status == task.Done {
		if len(attemptIDs) > 0 && attemptIDs[0] != "" {
			if _, err := item.Attempt(attemptIDs[0]); err != nil {
				return nil, err
			}
		}
		if commit != "" {
			return nil, fmt.Errorf("%w: история done не переписывается", task.ErrTransition)
		}
		return &CompletionPreview{Task: item, NoChange: true, base: base, id: id}, nil
	}
	if item.Status != task.Todo && item.Status != task.Active && item.Status != task.Paused {
		return nil, task.ErrTransition
	}
	if err := p.git.CheckStart(ctx, false); err != nil {
		return nil, err
	}
	preview := &CompletionPreview{Task: item, base: base, id: id}
	if item.Status != task.Todo {
		a, err := p.selectAttempt(ctx, base.Plan, item, attemptIDs)
		if err != nil {
			return nil, err
		}
		preview.attemptID = a.ID
	} else if len(attemptIDs) > 0 && attemptIDs[0] != "" {
		return nil, task.ErrNotFound
	}
	if commit != "" {
		preview.commit, _, err = p.git.ResolveBase(ctx, commit)
		if err != nil {
			return nil, err
		}
		preview.target, err = p.git.BranchCommit(ctx, base.Plan.TargetBranch)
		if err != nil {
			return nil, err
		}
		if preview.target == "" {
			return nil, fmt.Errorf("целевая ветка отсутствует")
		}
		included, err := p.git.IsAncestor(ctx, preview.commit, preview.target)
		if err != nil {
			return nil, err
		}
		if !included {
			return nil, fmt.Errorf("commit не достижим из целевой ветки")
		}
		preview.Commit = preview.commit
	}
	return preview, nil
}

func (p *Plans) ApplyComplete(ctx context.Context, preview *CompletionPreview) (task.Task, error) {
	if preview == nil {
		return task.Task{}, fmt.Errorf("отсутствует подготовленное завершение")
	}
	current, err := p.PrepareComplete(ctx, preview.id, preview.commit, preview.attemptID)
	if err != nil {
		return task.Task{}, err
	}
	if !preview.base.SameVersion(current.base) || preview.target != current.target {
		return task.Task{}, storage.ErrConflict
	}
	if current.NoChange {
		return current.Task, nil
	}
	id := current.attemptID
	if id == "" {
		id, err = newOperationID()
		if err != nil {
			return task.Task{}, err
		}
	}
	next := current.base.Plan
	if _, err := next.CompleteManual(current.Task.ID, id, current.commit, time.Now().UTC()); err != nil {
		return task.Task{}, err
	}
	if _, err := p.save(ctx, current.base, next); err != nil {
		return task.Task{}, err
	}
	return next.FindID(current.Task.ID)
}
