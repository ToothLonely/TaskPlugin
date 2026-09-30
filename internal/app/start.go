package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
)

// StartOptions contains validated syntax, independent of any terminal UI.
type StartOptions struct {
	Branch    string
	ID        string
	Title     string
	New       string
	From      string
	Again     bool
	Selection *StartSelection
}

type startIntent struct {
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id"`
	Branch       string   `json:"branch"`
	Base         string   `json:"base"`
	BaseRef      string   `json:"base_ref,omitempty"`
	Target       string   `json:"target"`
	TargetCommit string   `json:"target_commit"`
	Head         git.Head `json:"head"`
	HeadLog      string   `json:"head_log,omitempty"`
	BranchLog    string   `json:"branch_log,omitempty"`
}

func newOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (p *Plans) point(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.checkpoint != nil {
		return p.checkpoint(name)
	}
	return nil
}

func selectStart(plan *task.Plan, options StartOptions) (task.Task, error) {
	selectors := 0
	for _, v := range []string{options.ID, options.Title, options.New} {
		if v != "" {
			selectors++
		}
	}
	if options.Branch == "" || selectors > 1 || options.Again && (options.ID == "" && options.Title == "") {
		return task.Task{}, fmt.Errorf("неверные параметры start")
	}
	var selected task.Task
	var err error
	switch {
	case options.ID != "":
		selected, err = plan.FindID(options.ID)
	case options.Title != "":
		selected, err = plan.FindTitle(options.Title)
	case options.New != "":
		selected, err = plan.Add(options.New, "", task.Position{})
	default:
		selected, err = plan.FirstTodo()
	}
	if errors.Is(err, task.ErrAmbiguous) {
		return task.Task{}, fmt.Errorf("%w; укажите --id", err)
	}
	if err != nil {
		return task.Task{}, err
	}
	if selected.Status == task.Done && !options.Again {
		selector := fmt.Sprintf("--id %q", options.ID)
		if options.Title != "" {
			selector = fmt.Sprintf("--title %q", options.Title)
		}
		return task.Task{}, fmt.Errorf("%w; git task start %q %s --again", task.ErrAgainRequired, options.Branch, selector)
	}
	return selected, nil
}

// Start creates a branch and records a single attempt only after verified Git
// effects. All selectors converge here on the permanent task ID.
func (p *Plans) Start(ctx context.Context, options StartOptions) (result task.Task, err error) {
	err = p.store.WithOperation(ctx, func(op *storage.Operation) error {
		base, err := op.Load()
		if err != nil {
			return err
		}
		if base.PendingOperation {
			return storage.ErrOperation
		}
		if options.Selection != nil {
			if !base.SameVersion(options.Selection.snapshot) {
				return fmt.Errorf("%w; повторите --select", storage.ErrConflict)
			}
			if err := p.unchangedBase(ctx, options.Selection.intent); err != nil {
				return fmt.Errorf("основание меню изменилось; повторите --select: %w", err)
			}
			if err := p.sameHead(ctx, options.Selection.intent.Head); err != nil {
				return fmt.Errorf("повторите --select: %w", err)
			}
		}
		next := base.Plan
		selected, err := selectStart(&next, options)
		if err != nil {
			return err
		}
		if _, err = p.git.BranchRef(ctx, options.Branch); err != nil {
			return err
		}
		existing, err := p.git.BranchCommit(ctx, options.Branch)
		if err != nil {
			return err
		}
		if existing != "" {
			return fmt.Errorf("ветка %q уже существует", options.Branch)
		}
		intent, err := p.prepareIntent(ctx, "start", selected.ID, options.Branch, base.Plan.TargetBranch, options.From)
		if err != nil {
			return err
		}
		if intent.Head.Ref == "" {
			return fmt.Errorf("HEAD отсоединён; сначала явно выберите ветку")
		}
		now := time.Now().UTC()
		attempt := task.Attempt{ID: intent.ID, Branch: intent.Branch, OriginalBranch: intent.Branch, TargetBranch: intent.Target, BaseCommit: intent.Base, StartedAt: &now}
		attempt.Observation, err = p.baseline(ctx, "", intent.Base, intent.Target, intent.TargetCommit)
		if err != nil {
			return err
		}
		if _, err = next.Start(selected.ID, attempt, options.Again); err != nil {
			return err
		}
		if err = p.git.CheckStart(ctx, true); err != nil {
			return err
		}
		if err = p.git.CheckTreeStorage(ctx, intent.Base); err != nil {
			return err
		}
		journal, err := op.Prepare(ctx, base, next, intent)
		if err != nil {
			return err
		}
		if err = p.point(ctx, "prepared"); err != nil {
			return err
		}
		if err = p.unchangedBase(ctx, intent); err != nil {
			return err
		}
		if err = p.sameHead(ctx, intent.Head); err != nil {
			return err
		}
		if _, err = op.State(journal); err != nil {
			return err
		}
		if err = p.git.CreateBranch(ctx, intent.Branch, intent.Base, intent.ID); err != nil {
			// Git can fail after an effect. Only proven absence permits closing.
			probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			branch, readErr := p.git.BranchCommit(probe, intent.Branch)
			if readErr == nil && branch == "" && p.sameHead(probe, intent.Head) == nil {
				return errors.Join(err, op.Close(journal))
			}
			return errors.Join(err, readErr, storage.ErrOperation)
		}
		if err = p.point(ctx, "created"); err != nil {
			return err
		}
		// A hook or another Git process may have replaced the new ref. Check
		// before switching too: a foreign tree could overwrite local metadata.
		owned, err := p.git.OwnsBranch(ctx, intent.Branch, intent.Base, intent.ID)
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("созданная ветка изменена до checkout: %w", storage.ErrOperation)
		}
		if err = p.unchangedBase(ctx, intent); err != nil {
			return err
		}
		if err = p.sameHead(ctx, intent.Head); err != nil {
			return err
		}
		if err = p.git.CheckStart(ctx, true); err != nil {
			return err
		}
		if _, err = op.State(journal); err != nil {
			return err
		}
		checkoutErr := p.git.Checkout(ctx, intent.Branch, intent.ID)
		if err = p.point(ctx, "checked-out"); err != nil {
			return errors.Join(checkoutErr, err)
		}
		// Successful work keeps the caller's context. A short recovery probe
		// timeout must not impose a hidden deadline on normal Git/storage I/O.
		if err = p.verifyEffect(ctx, intent); err != nil {
			return errors.Join(checkoutErr, err, storage.ErrOperation)
		}
		if err = op.Commit(ctx, journal); err != nil {
			return errors.Join(checkoutErr, err)
		}
		result, _ = next.FindID(selected.ID)
		if err = p.point(ctx, "committed"); err != nil {
			return errors.Join(checkoutErr, err)
		}
		if err = op.Close(journal); err != nil {
			return fmt.Errorf("задача уже запущена; журнал не закрыт: %w", errors.Join(checkoutErr, err))
		}
		if checkoutErr != nil {
			return fmt.Errorf("задача уже запущена; Git/hook сообщил ошибку: %w", checkoutErr)
		}
		return nil
	})
	return result, err
}

func (p *Plans) prepareIntent(ctx context.Context, kind, id, branch, target, from string) (startIntent, error) {
	in := startIntent{Kind: kind, TaskID: id, Branch: branch, Target: target}
	var err error
	in.TargetCommit, err = p.git.BranchCommit(ctx, target)
	if err != nil {
		return in, err
	}
	if in.TargetCommit == "" {
		return in, fmt.Errorf("целевая ветка %q отсутствует", target)
	}
	in.Base, in.BaseRef = in.TargetCommit, "refs/heads/"+target
	if from != "" {
		in.Base, in.BaseRef, err = p.git.ResolveBase(ctx, from)
		if err != nil {
			return in, err
		}
	}
	in.Head, err = p.git.HeadState(ctx)
	if err != nil {
		return in, err
	}
	in.ID, err = newOperationID()
	return in, err
}

func (p *Plans) sameHead(ctx context.Context, expected git.Head) error {
	actual, err := p.git.HeadState(ctx)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("HEAD изменён; требуется восстановление")
	}
	return nil
}

func (p *Plans) unchangedBase(ctx context.Context, in startIntent) error {
	target, err := p.git.BranchCommit(ctx, in.Target)
	if err != nil {
		return err
	}
	if target != in.TargetCommit {
		return fmt.Errorf("целевая ветка изменена; требуется восстановление")
	}
	if in.BaseRef != "" {
		actual, _, err := p.git.ResolveBase(ctx, in.BaseRef)
		if err != nil {
			return err
		}
		if actual != in.Base {
			return fmt.Errorf("основание изменено; требуется восстановление")
		}
	}
	return nil
}

func (p *Plans) verifyEffect(ctx context.Context, in startIntent) error {
	if err := p.unchangedBase(ctx, in); err != nil {
		return err
	}
	if in.Kind == "resume" {
		if err := p.verifyResumeBranch(ctx, in); err != nil {
			return err
		}
		owned, err := p.git.OwnsResume(ctx, in.Branch, in.Base, in.ID)
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("переключение resume не подтверждено: %w", storage.ErrOperation)
		}
		return nil
	}
	if in.Kind == "start" {
		owned, err := p.git.OwnsBranch(ctx, in.Branch, in.Base, in.ID)
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("владение созданной веткой не подтверждено")
		}
		return p.sameHead(ctx, git.Head{Ref: "refs/heads/" + in.Branch, Commit: in.Base})
	}
	actual, err := p.git.BranchCommit(ctx, in.Branch)
	if err != nil {
		return err
	}
	if actual != in.Base {
		return fmt.Errorf("привязываемая ветка изменена")
	}
	return nil
}
