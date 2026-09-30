package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestCompleteManualCommitNoOpAndHistory(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	tip, _ := c.BranchCommit(ctx, "work")
	before := planBytes(t, c)
	if _, err := p.PrepareComplete(ctx, started.ID, tip); err == nil || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatal("unreachable commit accepted")
	}
	mergeBranch(t, c, "work", true)
	preview, err := p.PrepareComplete(ctx, started.ID, tip)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, planBytes(t, c)) {
		t.Fatal("preview changed plan")
	}
	preview.Task.ID = "task-003"
	preview.Commit = strings.Repeat("a", 40)
	done, err := p.ApplyComplete(ctx, preview)
	if err != nil {
		t.Fatal(err)
	}
	if done.ID != started.ID || done.Status != task.Done || done.ActiveAttempt != nil || len(done.Attempts) != 1 || done.Attempts[0].ID != started.ActiveAttempt.ID || done.Attempts[0].Completion.Source != task.Manual || done.Attempts[0].Completion.Commit != tip {
		t.Fatalf("manual completion: %+v", done)
	}
	before = planBytes(t, c)
	preview, err = p.PrepareComplete(ctx, started.ID, "")
	if err != nil || !preview.NoChange {
		t.Fatalf("no-op: %v %v", preview, err)
	}
	if _, err := p.ApplyComplete(ctx, preview); err != nil || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("repeat: %v", err)
	}
	if _, err := p.PrepareComplete(ctx, started.ID, tip); !errors.Is(err, task.ErrTransition) {
		t.Fatalf("changed evidence for done: %v", err)
	}
}

func TestCompletePreviewConflictsAndStatusRules(t *testing.T) {
	for _, scenario := range []string{"todo", "paused", "archived", "conflict", "missing-commit", "invalid-commit"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			if scenario == "paused" {
				if _, err := p.Start(ctx, StartOptions{Branch: "work"}); err != nil {
					t.Fatal(err)
				}
				editPlan(t, p, func(plan *task.Plan) error { _, err := plan.Pause("task-001"); return err })
			}
			if scenario == "archived" {
				editPlan(t, p, func(plan *task.Plan) error { _, err := plan.Archive("task-001"); return err })
			}
			commit := ""
			if scenario == "missing-commit" {
				commit = strings.Repeat("f", 40)
			}
			if scenario == "invalid-commit" {
				commit = "--all"
			}
			before := planBytes(t, c)
			preview, err := p.PrepareComplete(ctx, "task-001", commit)
			if scenario == "archived" || commit != "" {
				if err == nil || !bytes.Equal(before, planBytes(t, c)) {
					t.Fatal("invalid complete changed plan")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "conflict" {
				if _, err := p.Add(ctx, "other", "", task.Position{}); err != nil {
					t.Fatal(err)
				}
				before = planBytes(t, c)
				if _, err := p.ApplyComplete(ctx, preview); !errors.Is(err, storage.ErrConflict) || !bytes.Equal(before, planBytes(t, c)) {
					t.Fatalf("conflict: %v", err)
				}
				return
			}
			item, err := p.ApplyComplete(ctx, preview)
			if err != nil || item.Status != task.Done || len(item.Attempts) != 1 || item.Attempts[0].Completion.Source != task.Manual {
				t.Fatalf("manual: %+v %v", item, err)
			}
			if scenario == "todo" && (item.Attempts[0].Branch != "" || item.Attempts[0].StartedAt != nil || item.Attempts[0].Completion.Commit != "") {
				t.Fatal("fabricated Git evidence")
			}
		})
	}
}
