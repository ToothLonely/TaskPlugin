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

// Attach binds an existing branch without switching, or explicitly rebinds an
// active/paused attempt. A repeated ordinary attach is rejected by the model.
func (p *Plans) Attach(ctx context.Context, branch, id string, rebind bool, attemptIDs ...string) (result task.Task, err error) {
	err = p.store.WithOperation(ctx, func(op *storage.Operation) error {
		base, err := op.Load()
		if err != nil {
			return err
		}
		if base.PendingOperation {
			return storage.ErrOperation
		}
		selected, err := selectTaskID(base.Plan, id)
		if err != nil {
			return err
		}
		id = selected.ID
		if branch == base.Plan.TargetBranch {
			return fmt.Errorf("нельзя привязать целевую ветку")
		}
		commit, err := p.git.BranchCommit(ctx, branch)
		if err != nil {
			return err
		}
		if commit == "" {
			return fmt.Errorf("ветка %q отсутствует", branch)
		}
		if err = p.git.CheckStart(ctx, false); err != nil {
			return err
		}
		in, err := p.prepareIntent(ctx, "attach", id, branch, base.Plan.TargetBranch, "refs/heads/"+branch)
		if err != nil {
			return err
		}
		next := base.Plan
		now := time.Now().UTC()
		changed := false
		if rebind {
			in.Kind = "rebind"
			bound, findErr := next.FindID(id)
			if findErr != nil {
				return findErr
			}
			a, selectErr := p.selectAttempt(ctx, next, bound, attemptIDs)
			if selectErr != nil {
				return selectErr
			}
			if a.Branch == branch {
				log, logErr := p.git.BranchLog(ctx, branch)
				if logErr != nil && !errors.Is(logErr, git.ErrHistoryUnavailable) {
					return logErr
				}
				if logErr == nil && tracking.BindingContinuous(a, log, commit) {
					result = bound
					return nil
				}
			}
			changed, err = next.Rebind(id, branch, commit, now, a.ID)
			in.ID = a.ID
			markLocal(&next, in.ID)
		} else {
			author, authorErr := p.author(ctx)
			if authorErr != nil {
				return authorErr
			}
			changed, err = next.Attach(id, task.Attempt{ID: in.ID, Author: author, Branch: branch, OriginalBranch: branch, TargetBranch: in.Target, BaseCommit: commit, StartedAt: &now})
			markLocal(&next, in.ID)
		}
		if err != nil {
			return err
		}
		if !changed {
			result, _ = next.FindID(id)
			return nil
		}
		observation, err := p.baseline(ctx, branch, commit, in.Target, in.TargetCommit)
		if err != nil {
			return err
		}
		if _, err = next.Observe(id, in.ID, observation, nil); err != nil {
			return err
		}
		journal, err := op.Prepare(ctx, base, next, in)
		if err != nil {
			return err
		}
		if err = p.point(ctx, "prepared"); err != nil {
			return err
		}
		if err = p.verifyEffect(ctx, in); err != nil {
			return err
		}
		if err = p.commit(ctx, op, journal); err != nil {
			return err
		}
		result, _ = next.FindID(id)
		if err = p.point(ctx, "committed"); err != nil {
			return err
		}
		return op.Close(journal)
	})
	return result, err
}
