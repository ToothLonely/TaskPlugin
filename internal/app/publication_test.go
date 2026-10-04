package app

import (
	"context"
	"testing"

	"git-task/internal/task"
)

func TestCommandRecordsOnlyItsOwnSuccessfulWrites(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	first, second := f.a.ForCommand(), f.a.ForCommand()
	if _, err := second.Add(ctx, "Other command action", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	before, err := f.a.store.Load(ctx)
	if err != nil || len(before.Plan.Team.Pending) != 1 || len(first.actionIDs) != 0 || len(second.actionIDs) != 1 {
		t.Fatalf("command isolation: %v", err)
	}
	if err := first.PublishRecordedActions(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || !before.SameVersion(after) {
		t.Fatalf("empty command published foreign receipt: %v", err)
	}
	if _, err := first.Add(ctx, "Own command action", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.a.store.Load(ctx)
	if err != nil || len(first.actionIDs) != 1 || len(snapshot.Plan.Team.Pending) != 2 || first.actionIDs[0] != snapshot.Plan.Team.Pending[1].ID || first.actionIDs[0] == second.actionIDs[0] {
		t.Fatalf("write receipt attribution: %v", err)
	}
	if fresh := first.ForCommand(); len(fresh.actionIDs) != 0 {
		t.Fatal("previous command receipt leaked into next command")
	}
	if err := first.PublishRecordedActions(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := f.a.store.Load(ctx)
	if err != nil || len(final.Plan.Team.Pending) != 0 {
		t.Fatalf("own confirmed mutation not published: %v", err)
	}
}
