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
	err = p.store.WithOperation(ctx, func(op *storage.Operation) error {
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
		if err = op.Commit(ctx, journal); err != nil {
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
	})
	return recovered, err
}
