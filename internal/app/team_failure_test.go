package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/git"
	"git-task/internal/task"
)

func TestTeamCancellationAndServerDenialKeepQueue(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Add(ctx, "Queued action", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	base, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := f.a.Publish(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	hook := filepath.Join(f.remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' 'permission denied by isolated server' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	f.a.checkpoint = func(name string) error {
		if name == "publish-before-push" {
			attempts++
		}
		return nil
	}
	if err := f.a.Publish(ctx); err == nil || errors.Is(err, git.ErrPushRace) {
		t.Fatalf("server denial classified as race: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("denied push was retried: %d", attempts)
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || !base.SameVersion(after) {
		t.Fatalf("denial changed local queue: %v", err)
	}
}

func TestPublicationDeadlineFailureKeepsActionIDsForNextPublish(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Add(ctx, "Deadline queued action", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	before, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.a.checkpoint = func(point string) error {
		if point == "publish-before-push" {
			return context.DeadlineExceeded
		}
		return nil
	}
	if err := f.a.Publish(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline failure: %v", err)
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || !before.SameVersion(after) {
		t.Fatalf("deadline changed queued action: %v", err)
	}
	assertDoctorQueueReadOnly(t, f.a, f.ca)
	f.a.checkpoint = nil
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := f.a.store.Load(ctx)
	if err != nil || len(final.Plan.Team.Pending) != 0 || len(final.Plan.Tasks) != 2 {
		t.Fatalf("deadline recovery duplicated/lost work: %v", err)
	}
	id := before.Plan.Team.Pending[0].ID
	count := 0
	for _, r := range final.Plan.Actions {
		if r.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("original action receipt count: %d", count)
	}
}

func TestTeamCorruptRemoteDoesNotReplaceLocalQueue(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.b.Add(ctx, "Offline task", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	base, err := f.b.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := f.ca.FetchPlan(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := f.ca.PlanCommit(ctx, tip, []byte(`{"format":"git-task","schema_version":99}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ca.PushPlan(ctx, "origin", tip, commit); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Publish(ctx); err == nil || errors.Is(err, git.ErrPushRace) || errors.Is(err, task.ErrSharedConflict) {
		t.Fatalf("corruption not distinguished: %v", err)
	}
	after, err := f.b.store.Load(ctx)
	if err != nil || !base.SameVersion(after) || len(after.Plan.Team.Pending) != 1 {
		t.Fatalf("local queue damaged: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(f.cb.Dir, ".git-task", "conflict-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("corrupt version not preserved: %v %v", files, err)
	}
}
