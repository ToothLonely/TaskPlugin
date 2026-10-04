package app

import (
	"context"
	"testing"

	"git-task/internal/task"
)

func TestFreshRemoteCannotErasePublishedCompletion(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	item, err := f.a.Start(ctx, StartOptions{Branch: "stale-work", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := task.SharedBytes(active.Plan)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.a.PrepareComplete(ctx, f.id, "", item.ActiveAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.ApplyComplete(ctx, preview); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Plan.Team.Pending) != 0 {
		t.Fatal("completion publication was not acknowledged")
	}
	tip, err := f.ca.FetchPlan(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := f.ca.PlanCommit(ctx, tip, stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ca.PushPlan(ctx, "origin", tip, commit); err != nil {
		t.Fatal(err)
	}
	if err := f.a.FetchTeam(ctx); err == nil {
		after, loadErr := f.a.store.Load(ctx)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		got, findErr := after.Plan.FindID(f.id)
		if findErr != nil {
			t.Fatal(findErr)
		}
		t.Fatalf("stale remote accepted: task=%s completion=%v", got.Status, got.Attempts[0].Completion)
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || !before.SameVersion(after) {
		t.Fatalf("conflicting remote changed local completion: %v", err)
	}
}
