package app

import (
	"context"
	"strings"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestPartialCompletionWithoutCursorThroughApplication(t *testing.T) {
	for _, mode := range []string{"manual", "tracking", "server"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			a, err := f.a.Start(ctx, StartOptions{Branch: "partial-a", ID: f.id})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.a.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := f.b.Start(ctx, StartOptions{Branch: "partial-b", ID: f.id}); err != nil {
				t.Fatal(err)
			}
			if err := f.b.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			if err := f.a.FetchTeam(ctx); err != nil {
				t.Fatal(err)
			}
			base, err := f.a.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			next := base.Plan
			next.InsertionTail = ""
			next.Revision++
			if _, err := f.a.store.Save(ctx, base, next); err != nil {
				t.Fatal(err)
			}
			if err := f.a.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "manual":
				preview, err := f.a.PrepareComplete(ctx, f.id, "", a.ActiveAttempt.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.a.ApplyComplete(ctx, preview); err != nil {
					t.Fatal(err)
				}
			default:
				before := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main")))
				testrepo.Commit(t, f.ca)
				if mode == "tracking" {
					if _, _, err := f.a.Sync(ctx); err != nil {
						t.Fatal(err)
					}
				}
				testrepo.Run(t, f.ca, "checkout", "main")
				testrepo.Run(t, f.ca, "merge", "--ff-only", "partial-a")
				if mode == "tracking" {
					if _, _, err := f.a.Sync(ctx); err != nil {
						t.Fatal(err)
					}
				} else {
					teamNetwork(t, f.ca, "push", "origin", "refs/heads/partial-a:refs/heads/partial-a", "refs/heads/main:refs/heads/main")
					after := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main")))
					if err := f.a.ReconcileTeam(ctx, before, after); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := f.a.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			item, err := result.Plan.FindID(f.id)
			if err != nil || item.Status != task.Active || result.Plan.InsertionTail != "" || len(item.Attempts) != 2 {
				t.Fatalf("partial result: %+v %v", result.Plan, err)
			}
			done, live := 0, 0
			for _, approach := range item.Attempts {
				if approach.ID == a.ActiveAttempt.ID && approach.Status == task.Done && approach.Completion != nil {
					done++
				}
				if approach.ID != a.ActiveAttempt.ID && approach.Status == task.Active && approach.Completion == nil {
					live++
				}
			}
			if done != 1 || live != 1 {
				t.Fatal("partial completion changed the other approach")
			}
		})
	}
}
