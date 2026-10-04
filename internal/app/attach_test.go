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

func TestSameNameRebindAfterBranchRecreation(t *testing.T) {
	for _, scenario := range []struct {
		name                        string
		sameSHA, syncBefore, paused bool
	}{
		{"same-sha-observed", true, true, false},
		{"different-sha-observed", false, true, false},
		{"same-sha-unobserved-paused", true, false, true},
		{"different-sha-unobserved-paused", false, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			testrepo.Commit(t, c)
			observed := syncTask(t, p, started.ID)
			oldWork := observed.ActiveAttempt.Observation.WorkCommit
			if scenario.paused {
				editPlan(t, p, func(plan *task.Plan) error { _, err := plan.Pause(started.ID); return err })
			}
			testrepo.Run(t, c, "checkout", "main")
			testrepo.Run(t, c, "branch", "-D", "work")
			base := "main"
			if scenario.sameSHA {
				base = observed.ActiveAttempt.Observation.Tip
			}
			testrepo.Run(t, c, "branch", "work", base)
			if scenario.syncBefore {
				uncertain := syncTask(t, p, started.ID)
				if len(uncertain.Warnings) != 1 || uncertain.Warnings[0].Code != "binding_uncertain" {
					t.Fatalf("recreated branch not diagnosed: %+v", uncertain)
				}
			}
			head, err := c.HeadState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			rebound, err := p.Attach(ctx, "work", started.ID, true)
			if err != nil {
				t.Fatal(err)
			}
			a := rebound.ActiveAttempt
			status := task.Active
			if scenario.paused {
				status = task.Paused
			}
			if rebound.Status != status || a.ID != started.ActiveAttempt.ID || a.OriginalBranch != "work" || len(a.Rebindings) != 1 || a.Rebindings[0].From != "work" || a.Rebindings[0].To != "work" || a.Observation.WorkCommit != "" || a.Observation.BranchLog == observed.ActiveAttempt.Observation.BranchLog || len(rebound.Warnings) != 0 {
				t.Fatalf("renewal lost identity or reused evidence: %+v", rebound)
			}
			after, err := c.HeadState(ctx)
			if err != nil || after != head {
				t.Fatalf("rebind switched HEAD: %v %v", after, err)
			}
			before := planBytes(t, c)
			if _, err := p.Attach(ctx, "work", started.ID, true); err != nil || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("confirmed rebind not no-op: %v", err)
			}
			fresh := syncTask(t, p, started.ID)
			if fresh.Status != status || fresh.ActiveAttempt.Observation.WorkCommit != "" || len(fresh.Attempts) != 1 || len(fresh.Warnings) != 0 {
				t.Fatalf("old work inherited by new binding: %+v", fresh)
			}
			mergeBranch(t, c, "work", true)
			fresh = syncTask(t, p, started.ID)
			if fresh.Status != status || fresh.ActiveAttempt.Observation.WorkCommit != "" || len(fresh.Attempts) != 1 {
				t.Fatalf("old work integration completed renewed binding: %+v", fresh)
			}
			testrepo.Run(t, c, "checkout", "work")
			testrepo.Commit(t, c)
			fresh = syncTask(t, p, started.ID)
			if fresh.ActiveAttempt.Observation.WorkCommit == "" || fresh.ActiveAttempt.Observation.WorkCommit == oldWork {
				t.Fatalf("fresh work not observed: %+v", fresh)
			}
			testrepo.Commit(t, c)
			before = planBytes(t, c)
			if _, err := p.Attach(ctx, "work", started.ID, true); err != nil || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("continuous advanced binding not no-op: %v", err)
			}
			mergeBranch(t, c, "work", true)
			done := syncTask(t, p, started.ID)
			if done.Status != task.Done || len(done.Attempts) != 1 || done.Attempts[0].ID != started.ActiveAttempt.ID || len(done.Attempts[0].Rebindings) != 1 || done.Attempts[0].Observation.WorkCommit == oldWork {
				t.Fatalf("renewed binding completion: %+v", done)
			}
		})
	}
}

func TestSameNameRebindRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			testrepo.Commit(t, c)
			observed := syncTask(t, p, started.ID)
			testrepo.Run(t, c, "checkout", "main")
			testrepo.Run(t, c, "branch", "-D", "work")
			testrepo.Run(t, c, "branch", "work", observed.ActiveAttempt.Observation.Tip)
			before := planBytes(t, c)
			stop := errors.New("interrupted renewal")
			p.checkpoint = func(point string) error {
				if point == phase {
					return stop
				}
				return nil
			}
			if _, err := p.Attach(ctx, "work", started.ID, true); !errors.Is(err, stop) {
				t.Fatalf("renewal interruption: %v", err)
			}
			if phase == "prepared" && !bytes.Equal(before, planBytes(t, c)) {
				t.Fatal("prepared renewal changed installed plan")
			}
			p.checkpoint = nil
			if recovered, err := p.RecoverStart(ctx); err != nil || !recovered {
				t.Fatalf("recover renewal: %v %v", recovered, err)
			}
			recovered := savedTask(t, p, started.ID)
			if recovered.ActiveAttempt.ID != started.ActiveAttempt.ID || len(recovered.ActiveAttempt.Rebindings) != 1 || recovered.ActiveAttempt.Observation.WorkCommit != "" || len(recovered.Attempts) != 1 {
				t.Fatalf("recovery lost identity or reused evidence: %+v", recovered)
			}
			before = planBytes(t, c)
			if _, err := p.Attach(ctx, "work", started.ID, true); err != nil || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("recovered confirmed binding not no-op: %v", err)
			}
		})
	}
}
