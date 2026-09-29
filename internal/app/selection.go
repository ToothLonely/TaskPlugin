package app

import (
	"context"
	"fmt"

	"git-task/internal/storage"
	"git-task/internal/task"
)

type StartSelection struct {
	Plan     task.Plan
	Base     string
	snapshot storage.Snapshot
	intent   startIntent
}

func (p *Plans) PrepareSelection(ctx context.Context, options StartOptions) (*StartSelection, error) {
	snapshot, err := p.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot.PendingOperation {
		return nil, storage.ErrOperation
	}
	if _, err := p.git.BranchRef(ctx, options.Branch); err != nil {
		return nil, err
	}
	existing, err := p.git.BranchCommit(ctx, options.Branch)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, fmt.Errorf("ветка %q уже существует", options.Branch)
	}
	intent, err := p.prepareIntent(ctx, "start", "", options.Branch, snapshot.Plan.TargetBranch, options.From)
	if err != nil {
		return nil, err
	}
	return &StartSelection{Plan: snapshot.Plan, Base: intent.Base, snapshot: snapshot, intent: intent}, nil
}
