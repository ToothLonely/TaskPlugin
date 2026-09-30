package tracking

import (
	"context"
	"time"

	"git-task/internal/task"
)

func integration(ctx context.Context, reader Reader, tip, target string, o task.Observation, now time.Time) (*task.Completion, error) {
	commits, err := reader.FirstParentRange(ctx, target, o.TargetCommit)
	if err != nil {
		return nil, err
	}
	for _, commit := range commits {
		contains, err := reader.IsAncestor(ctx, tip, commit)
		if err != nil {
			return nil, err
		}
		if !contains {
			continue
		}
		kind := task.FastForward
		if commit != tip {
			parents, err := reader.Parents(ctx, commit)
			if err != nil {
				return nil, err
			}
			if len(parents) < 2 {
				return nil, nil
			}
			inFirst, err := reader.IsAncestor(ctx, tip, parents[0])
			if err != nil || inFirst {
				return nil, err
			}
			kind = task.MergeCommit
		}
		return &task.Completion{Source: task.Merge, MergeKind: kind, Commit: commit, WorkCommit: tip, ObservedAt: &now}, nil
	}
	return nil, nil
}
