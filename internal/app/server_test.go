package app

import (
	"context"
	"strings"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestServerCompletionRacesDeveloperPublication(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	before := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main")))
	if _, err := f.a.Start(ctx, StartOptions{Branch: "server-work", ID: f.id}); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, f.ca)
	teamNetwork(t, f.ca, "push", "origin", "refs/heads/server-work:refs/heads/server-work")
	testrepo.Run(t, f.ca, "checkout", "main")
	testrepo.Run(t, f.ca, "merge", "--ff-only", "server-work")
	after := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main")))
	teamNetwork(t, f.ca, "push", "origin", "refs/heads/main:refs/heads/main")
	traced := false
	f.a.checkpoint = func(name string) error {
		if name != "publish-before-push" || traced {
			return nil
		}
		traced = true
		if _, err := f.b.Add(ctx, "Concurrent developer action", "", task.Position{}); err != nil {
			return err
		}
		return f.b.Publish(ctx)
	}
	if err := f.a.ReconcileTeam(ctx, before, after); err != nil {
		t.Fatal(err)
	}
	if !traced {
		t.Fatal("race checkpoint was not reached")
	}
	if err := f.b.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := f.b.store.Load(ctx)
	if err != nil || len(s.Plan.Tasks) != 2 {
		t.Fatalf("developer action lost: %+v %v", s.Plan, err)
	}
	item, err := s.Plan.FindID(f.id)
	if err != nil || item.Status != task.Done || len(item.Attempts) != 1 || item.Attempts[0].Completion.TargetBefore != before {
		t.Fatalf("server completion lost: %+v %v", item, err)
	}
}

func TestForeignApproachIsNotLocalBranchAssignment(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Start(ctx, StartOptions{Branch: "foreign-work", ID: f.id}); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.b.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, f.cb, "checkout", "-b", "foreign-work")
	if _, _, err := f.b.Pause(ctx, f.id); err == nil {
		t.Fatal("foreign approach selected as local")
	}
	r, err := f.b.StatusReport(ctx)
	if err != nil || r.CurrentTaskID != nil {
		t.Fatalf("manual branch assigned foreign task: %+v %v", r, err)
	}
	item, _, err := f.b.Show(ctx, f.id)
	if err != nil || item.Status != task.Active || item.Attempts[0].Observation != nil {
		t.Fatalf("foreign refs used for tracking: %+v %v", item, err)
	}
}
