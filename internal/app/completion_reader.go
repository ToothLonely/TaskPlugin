package app

import "context"

type completionReader interface {
	BranchCommit(context.Context, string) (string, error)
	HasObject(context.Context, string) (bool, error)
	IsAncestor(context.Context, string, string) (bool, error)
}

type completionCache struct {
	source    completionReader
	branches  map[string]string
	objects   map[string]bool
	ancestors map[[2]string]bool
}

func newCompletionReader(source completionReader) *completionCache {
	return &completionCache{source: source, branches: map[string]string{}, objects: map[string]bool{}, ancestors: map[[2]string]bool{}}
}

func (r *completionCache) BranchCommit(ctx context.Context, branch string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if value, ok := r.branches[branch]; ok {
		return value, nil
	}
	value, err := r.source.BranchCommit(ctx, branch)
	if err == nil {
		r.branches[branch] = value
	}
	return value, err
}

func (r *completionCache) HasObject(ctx context.Context, oid string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if value, ok := r.objects[oid]; ok {
		return value, nil
	}
	value, err := r.source.HasObject(ctx, oid)
	if err == nil {
		r.objects[oid] = value
	}
	return value, err
}

func (r *completionCache) IsAncestor(ctx context.Context, before, after string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	key := [2]string{before, after}
	if value, ok := r.ancestors[key]; ok {
		return value, nil
	}
	value, err := r.source.IsAncestor(ctx, before, after)
	if err == nil {
		r.ancestors[key] = value
	}
	return value, err
}
