package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"git-task/internal/task"
)

func TestForeignPublishedCompletionProtectedOnEveryReceivePath(t *testing.T) {
	for _, mode := range []string{"fetch", "cached", "publish"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			item, err := f.a.Start(ctx, StartOptions{Branch: "confirmed-work", ID: f.id})
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
			if err := f.b.FetchTeam(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := f.b.store.Load(ctx)
			if err != nil || len(before.Plan.Team.Pending) != 0 || len(before.Plan.Team.LocalAttempts) != 0 {
				t.Fatalf("foreign empty-queue prerequisite: %v", err)
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
			switch mode {
			case "fetch":
				err = f.b.FetchTeam(ctx)
			case "publish":
				err = f.b.Publish(ctx)
			case "cached":
				if _, err := f.cb.FetchPlan(ctx, "origin"); err != nil {
					t.Fatal(err)
				}
				err = f.b.ReceiveCached(ctx)
			}
			if !errors.Is(err, task.ErrSharedConflict) {
				t.Fatalf("rollback not rejected: %v", err)
			}
			after, err := f.b.store.Load(ctx)
			if err != nil || !before.SameVersion(after) {
				t.Fatalf("rollback changed local history: %v", err)
			}
			files, err := filepath.Glob(filepath.Join(f.cb.Dir, ".git-task", "conflict-*.json"))
			if err != nil || len(files) != 1 {
				t.Fatalf("conflict version missing: %v %v", files, err)
			}
		})
	}
}
