package app

import (
	"context"
	"encoding/json"
	"fmt"

	"git-task/internal/storage"
)

// RecoverStart is the explicit recovery operation for a future doctor UI.
// The caller is responsible for showing the diagnosis and obtaining consent.
// It never creates, deletes, or switches branches, and never guesses ownership.
func (p *Plans) RecoverStart(ctx context.Context) (recovered bool, err error) {
	return p.recoverStart(ctx, nil)
}

func (p *Plans) recoverStart(ctx context.Context, preview *storage.Repair) (recovered bool, err error) {
	recover := func(op *storage.Operation) error {
		if preview != nil {
			if err := p.store.CheckRepair(preview, true); err != nil {
				return err
			}
		}
		journal, err := op.Journal()
		if err != nil || journal == nil {
			return err
		}
		installed, err := op.State(journal)
		if err != nil {
			return err
		}
		var in startIntent
		if err = json.Unmarshal(journal.Intent, &in); err != nil {
			return err
		}
		if in.ID == "" || in.TaskID == "" || (in.Kind != "start" && in.Kind != "attach" && in.Kind != "rebind" && in.Kind != "resume") {
			return fmt.Errorf("неизвестное намерение операции")
		}
		if err = p.git.CheckStart(ctx, false); err != nil {
			return err
		}
		if in.Kind == "resume" && !installed && p.resumeUnchanged(ctx, in) == nil {
			if err = op.Close(journal); err != nil {
				return err
			}
			recovered = true
			return nil
		}
		if in.Kind == "start" && !installed {
			branch, err := p.git.BranchCommit(ctx, in.Branch)
			if err != nil {
				return err
			}
			if branch == "" {
				if err = p.sameHead(ctx, in.Head); err != nil {
					return err
				}
				if err = op.Close(journal); err != nil {
					return err
				}
				recovered = true
				return nil
			}
		}
		if err = p.verifyEffect(ctx, in); err != nil {
			return err
		}
		if err = p.commit(ctx, op, journal); err != nil {
			return err
		}
		if err = p.point(ctx, "recovered"); err != nil {
			return err
		}
		if err = op.Close(journal); err != nil {
			return fmt.Errorf("план сохранён; журнал не закрыт: %w", err)
		}
		recovered = true
		return nil
	}
	if preview == nil {
		err = p.store.WithOperation(ctx, recover)
	} else {
		err = p.store.WithRepairOperation(ctx, preview, recover)
	}
	return recovered, err
}
