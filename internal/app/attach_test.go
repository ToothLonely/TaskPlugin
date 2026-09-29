package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestAttachAndRebind(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	testrepo.Run(t, c, "branch", "existing")
	testrepo.Run(t, c, "branch", "replacement")
	if err := os.WriteFile(filepath.Join(c.Dir, "dirty"), []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	head, _ := c.HeadState(ctx)
	got, err := p.Attach(ctx, "existing", "task-001", false)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := c.HeadState(ctx)
	if after != head {
		t.Fatal("attach switched HEAD")
	}
	original := got.ActiveAttempt.ID
	before := planBytes(t, c)
	for _, call := range []struct {
		branch, id string
		rebind     bool
	}{
		{"existing", "task-001", false}, {"existing", "task-002", false}, {"main", "task-002", false}, {"missing", "task-002", false}, {"replacement", "task-002", true},
	} {
		if _, err = p.Attach(ctx, call.branch, call.id, call.rebind); err == nil {
			t.Fatalf("accepted %+v", call)
		}
		if !bytes.Equal(before, planBytes(t, c)) {
			t.Fatal("rejected attach changed plan")
		}
	}
	editPlan(t, p, func(plan *task.Plan) error { _, err := plan.Pause("task-001"); return err })
	got, err = p.Attach(ctx, "replacement", "task-001", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != task.Paused || got.ActiveAttempt.ID != original || got.ActiveAttempt.OriginalBranch != "existing" || len(got.ActiveAttempt.Rebindings) != 1 {
		t.Fatalf("bad rebind: %+v", got)
	}
	before = planBytes(t, c)
	if _, err = p.Attach(ctx, "replacement", "task-001", true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, planBytes(t, c)) {
		t.Fatal("rebind no-op rewrote plan")
	}
	markDone(t, p)
	if _, err = p.Attach(ctx, "existing", "task-002", false); err == nil {
		t.Fatal("attach reopened done")
	}
}

func TestAttachRecovery(t *testing.T) {
	t.Parallel()
	for _, rebind := range []bool{false, true} {
		for _, phase := range []string{"prepared", "committed"} {
			t.Run(phase+fmtBool(rebind), func(t *testing.T) {
				p, c := startFixture(t)
				ctx := context.Background()
				testrepo.Run(t, c, "branch", "existing")
				var previous string
				if rebind {
					got, err := p.Start(ctx, StartOptions{Branch: "original", ID: "task-001"})
					if err != nil {
						t.Fatal(err)
					}
					previous = got.ActiveAttempt.ID
				}
				stop := errors.New("interruption")
				p.checkpoint = func(point string) error {
					if point == phase {
						return stop
					}
					return nil
				}
				if _, err := p.Attach(ctx, "existing", "task-001", rebind); !errors.Is(err, stop) {
					t.Fatal(err)
				}
				p.checkpoint = nil
				if _, err := p.RecoverStart(ctx); err != nil {
					t.Fatal(err)
				}
				plan, err := p.Status(ctx)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := plan.FindID("task-001")
				if got.ActiveAttempt.Branch != "existing" || rebind && got.ActiveAttempt.ID != previous {
					t.Fatalf("recovered binding: %+v", got)
				}
			})
		}
	}
}
