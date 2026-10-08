package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/tracking"
)

func (p *Plans) Resume(ctx context.Context, id string, attemptIDs ...string) (result task.Task, err error) {
	err = p.store.WithOperation(ctx, func(op *storage.Operation) error {
		base, err := op.Load()
		if err != nil {
			return err
		}
		if base.PendingOperation {
			return storage.ErrOperation
		}
		next := base.Plan
		item, err := selectTaskID(next, id)
		if err != nil {
			return err
		}
		id = item.ID
		a, err := p.selectAttempt(ctx, next, item, attemptIDs)
		if err != nil {
			return err
		}
		if a.Status == task.Active {
			return fmt.Errorf("задача %s уже запущена: %w", id, task.ErrTransition)
		}
		if _, err = next.Resume(id, a.ID); err != nil {
			return err
		}
		branch := a.Branch
		commit, err := p.git.BranchCommit(ctx, branch)
		if err != nil {
			return err
		}
		if commit == "" {
			return fmt.Errorf("связанная ветка %q отсутствует; используйте attach <branch> --id %s --rebind", branch, id)
		}
		log, err := p.git.BranchLog(ctx, branch)
		if err != nil {
			return err
		}
		if !tracking.BindingContinuous(a, log, commit) {
			return fmt.Errorf("связь ветки %q не подтверждена; используйте attach <branch> --id %s --rebind", branch, id)
		}
		in, err := p.prepareIntent(ctx, "resume", id, branch, base.Plan.TargetBranch, "refs/heads/"+branch)
		if err != nil {
			return err
		}
		if in.Base != commit {
			return fmt.Errorf("ветка изменена до resume")
		}
		if in.Head.Ref == "" {
			return fmt.Errorf("HEAD отсоединён; сначала явно выберите ветку")
		}
		in.BranchLog = log.Digest()
		in.HeadLog, err = p.git.HeadLogEntry(ctx)
		if err != nil {
			return err
		}
		if err = p.checkResume(ctx, in); err != nil {
			return err
		}
		journal, err := op.Prepare(ctx, base, next, in)
		if err != nil {
			return err
		}
		if err = p.point(ctx, "prepared"); err != nil {
			return err
		}
		if err = p.checkResume(ctx, in); err != nil {
			return err
		}
		if _, err = op.State(journal); err != nil {
			return err
		}
		checkoutErr := p.git.CheckoutResume(ctx, branch, in.ID)
		if checkoutErr != nil {
			probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if p.resumeUnchanged(probe, in) == nil {
				return errors.Join(checkoutErr, op.Close(journal))
			}
			if effectErr := p.verifyEffect(probe, in); effectErr != nil {
				return errors.Join(checkoutErr, effectErr, storage.ErrOperation)
			}
		}
		if err = p.point(ctx, "checked-out"); err != nil {
			return fmt.Errorf("Git мог уже переключить ветку; требуется doctor: %w", errors.Join(checkoutErr, err, storage.ErrOperation))
		}
		if err = p.verifyEffect(ctx, in); err != nil {
			return errors.Join(checkoutErr, err, storage.ErrOperation)
		}
		if err = p.git.CheckStart(ctx, false); err != nil {
			return errors.Join(checkoutErr, err, storage.ErrOperation)
		}
		if err = p.commit(ctx, op, journal); err != nil {
			return fmt.Errorf("ветка уже переключена; требуется doctor: %w", errors.Join(checkoutErr, err, storage.ErrOperation))
		}
		result, _ = next.FindID(id)
		if err = p.point(ctx, "committed"); err != nil {
			return fmt.Errorf("задача уже продолжена; требуется doctor: %w", errors.Join(checkoutErr, err, storage.ErrOperation))
		}
		if err = op.Close(journal); err != nil {
			return fmt.Errorf("задача уже продолжена; журнал не закрыт: %w", errors.Join(checkoutErr, err))
		}
		if checkoutErr != nil {
			return fmt.Errorf("задача уже продолжена; Git/hook сообщил ошибку: %w", checkoutErr)
		}
		return nil
	})
	return result, err
}

func (p *Plans) verifyResumeBranch(ctx context.Context, in startIntent) error {
	log, err := p.git.BranchLog(ctx, in.Branch)
	if err != nil {
		return err
	}
	if in.BranchLog == "" || log.Digest() != in.BranchLog {
		return fmt.Errorf("журнал связанной ветки изменён: %w", storage.ErrOperation)
	}
	return nil
}

func (p *Plans) resumeUnchanged(ctx context.Context, in startIntent) error {
	if err := p.sameHead(ctx, in.Head); err != nil {
		return err
	}
	entry, err := p.git.HeadLogEntry(ctx)
	if err != nil {
		return err
	}
	if entry != in.HeadLog {
		return fmt.Errorf("журнал HEAD изменён: %w", storage.ErrOperation)
	}
	if err := p.unchangedBase(ctx, in); err != nil {
		return err
	}
	return p.verifyResumeBranch(ctx, in)
}

func (p *Plans) checkResume(ctx context.Context, in startIntent) error {
	if err := p.resumeUnchanged(ctx, in); err != nil {
		return err
	}
	if err := p.git.CheckStart(ctx, true); err != nil {
		return err
	}
	return p.git.CheckTreeStorage(ctx, in.Base)
}
