package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/tracking"
)

func (p *Plans) baseline(ctx context.Context, branch, tip, target, targetCommit string) (*task.Observation, error) {
	o := &task.Observation{Tip: tip, TargetCommit: targetCommit}
	for _, item := range []struct {
		branch string
		dest   *string
	}{{branch, &o.BranchLog}, {target, &o.TargetLog}} {
		if item.branch == "" {
			continue
		}
		log, err := p.git.BranchLog(ctx, item.branch)
		if errors.Is(err, git.ErrHistoryUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		*item.dest = log.Digest()
	}
	return o, nil
}

func (p *Plans) Sync(ctx context.Context) (task.Plan, bool, error) {
	return p.syncCurrent(ctx, false)
}

func (p *Plans) SyncAfterMerge(ctx context.Context) (task.Plan, bool, error) {
	return p.syncCurrent(ctx, true)
}

func (p *Plans) syncCurrent(ctx context.Context, postMerge bool) (task.Plan, bool, error) {
	snapshot, err := p.store.Load(ctx)
	if err != nil {
		return task.Plan{}, false, err
	}
	if snapshot.PendingOperation {
		return snapshot.Plan, false, storage.ErrOperation
	}
	return p.sync(ctx, snapshot, postMerge)
}

func (p *Plans) sync(ctx context.Context, snapshot storage.Snapshot, postMerge bool) (task.Plan, bool, error) {
	needsTracking := false
	for _, t := range snapshot.Plan.Tasks {
		if t.Status == task.Active || t.Status == task.Paused {
			needsTracking = true
		}
		if t.Status == task.Done {
			for _, a := range t.Attempts {
				needsTracking = needsTracking || a.Completion.Source == task.Merge
			}
		}
	}
	if !needsTracking {
		return snapshot.Plan, false, nil
	}
	var blocked error
	checkState := func() error {
		if postMerge {
			return p.git.CheckPostMerge(ctx)
		}
		return p.git.CheckStart(ctx, false)
	}
	if err := checkState(); err != nil {
		if !errors.Is(err, git.ErrInProgress) {
			return task.Plan{}, false, err
		}
		blocked = err
	}
	next := snapshot.Plan
	type checked struct {
		attempt task.Attempt
		result  tracking.Result
	}
	var checks []checked
	now := time.Now().UTC()
	for _, id := range snapshot.Plan.Order {
		t, _ := snapshot.Plan.FindID(id)
		if t.Status == task.Done {
			warnings, err := p.completedWarnings(ctx, t)
			if err != nil {
				return task.Plan{}, false, err
			}
			if _, err = next.SetWarnings(id, warnings); err != nil {
				return task.Plan{}, false, err
			}
			continue
		}
		if t.Status != task.Active && t.Status != task.Paused {
			continue
		}
		a := *t.ActiveAttempt
		var result tracking.Result
		var err error
		if blocked != nil {
			result = tracking.Result{Observation: a.Observation, Warnings: []task.Warning{{Code: "git_in_progress", Message: blocked.Error()}}}
		} else {
			result, err = tracking.Observe(ctx, p.git, a, now)
			if err != nil {
				return task.Plan{}, false, err
			}
			checks = append(checks, checked{attempt: a, result: result})
		}
		if result.Completion != nil {
			_, err = next.Complete(id, a.ID, *result.Completion)
		} else {
			_, err = next.Observe(id, a.ID, result.Observation, result.Warnings)
		}
		if err != nil {
			return task.Plan{}, false, err
		}
	}
	if err := p.point(ctx, "sync-observed"); err != nil {
		return task.Plan{}, false, err
	}
	if next.Revision == snapshot.Plan.Revision {
		return next, false, ctx.Err()
	}
	for _, check := range checks {
		if err := check.result.Recheck(ctx, p.git, check.attempt); err != nil {
			return task.Plan{}, false, err
		}
	}
	if blocked == nil {
		if err := checkState(); err != nil {
			return task.Plan{}, false, fmt.Errorf("Git изменён во время сверки: %w", err)
		}
	}
	changed, err := p.store.Save(ctx, snapshot, next)
	return next, changed, err
}

func (p *Plans) completedWarnings(ctx context.Context, t task.Task) ([]task.Warning, error) {
	var warnings []task.Warning
	for _, a := range t.Attempts {
		c := a.Completion
		if c.Source != task.Merge {
			continue
		}
		target, err := p.git.BranchCommit(ctx, c.TargetBranch)
		if err != nil {
			return nil, err
		}
		lost := target == ""
		if !lost {
			for _, oid := range []string{c.Commit, c.WorkCommit} {
				exists, err := p.git.HasObject(ctx, oid)
				if err != nil {
					return nil, err
				}
				if !exists {
					lost = true
					break
				}
				included, err := p.git.IsAncestor(ctx, oid, target)
				if err != nil {
					return nil, err
				}
				if !included {
					lost = true
					break
				}
			}
		}
		if lost {
			warnings = append(warnings, task.Warning{Code: "completion_unreachable", Message: fmt.Sprintf("Доказательство подхода %s больше не достижимо из %s; история done сохранена.", a.ID, c.TargetBranch)})
		}
	}
	return warnings, nil
}
