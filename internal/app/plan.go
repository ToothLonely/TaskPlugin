// Package app coordinates repository checks, model operations and persistence.
package app

import (
	"context"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
)

// Plans provides the local plan operations shared by application entry points.
type Plans struct {
	store *storage.Store
	git   *git.Client
}

// Open resolves paths without assuming the process runs in the repository root.
func Open(ctx context.Context, dir string) (*Plans, error) {
	client, err := git.New(dir)
	if err != nil {
		return nil, err
	}
	repo, err := client.Discover(ctx)
	if err != nil {
		return nil, err
	}
	client.Dir = repo.Root
	return &Plans{store: storage.New(repo.Root, repo.ExcludePath, client), git: client}, nil
}

// Init returns whether a plan was created and whether HEAD has no commit.
func (p *Plans) Init(ctx context.Context, target string) (created, unborn bool, err error) {
	exists, err := p.git.HasCommit(ctx)
	if err != nil {
		return false, false, err
	}
	created, err = p.store.Init(ctx, target)
	return created, !exists, err
}

// Add validates and commits one domain operation against its source snapshot.
func (p *Plans) Add(ctx context.Context, title, description string, pos task.Position) (task.Task, error) {
	snapshot, err := p.store.Load(ctx)
	if err != nil {
		return task.Task{}, err
	}
	next := snapshot.Plan
	added, err := next.Add(title, description, pos)
	if err != nil {
		return task.Task{}, err
	}
	if _, err = p.store.Save(ctx, snapshot, next); err != nil {
		return task.Task{}, err
	}
	return added, nil
}

// Status reads the saved state; Git tracking is introduced in P09.
func (p *Plans) Status(ctx context.Context) (task.Plan, error) {
	snapshot, err := p.store.Load(ctx)
	return snapshot.Plan, err
}
