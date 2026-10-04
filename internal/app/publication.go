package app

import (
	"context"
	"slices"

	"git-task/internal/storage"
	"git-task/internal/task"
)

func (p *Plans) ForCommand() *Plans {
	command := *p
	command.actionIDs = nil
	command.notices = nil
	return &command
}

func (p *Plans) recordAction(id string) {
	if id != "" && !slices.Contains(p.actionIDs, id) {
		p.actionIDs = append(p.actionIDs, id)
	}
}

func (p *Plans) save(ctx context.Context, base storage.Snapshot, next task.Plan) (bool, error) {
	changed, id, err := p.store.SaveWithAction(ctx, base, next)
	p.recordAction(id)
	return changed, err
}

func (p *Plans) commit(ctx context.Context, op *storage.Operation, journal *storage.Journal) error {
	id, err := op.CommitWithAction(ctx, journal)
	p.recordAction(id)
	return err
}

func (p *Plans) PublishRecordedActions(ctx context.Context) error {
	if len(p.actionIDs) == 0 {
		return nil
	}
	snapshot, err := p.store.Load(ctx)
	if err != nil || snapshot.Plan.Team == nil || snapshot.PendingOperation {
		return err
	}
	for _, a := range snapshot.Plan.Team.Pending {
		if slices.Contains(p.actionIDs, a.ID) {
			return p.Publish(ctx)
		}
	}
	return nil
}
