package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func savedTask(t *testing.T, p *Plans, id string) task.Task {
	t.Helper()
	s, err := p.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.Plan.FindID(id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func syncTask(t *testing.T, p *Plans, id string) task.Task {
	t.Helper()
	plan, _, err := p.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	item, err := plan.FindID(id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func commitFile(t *testing.T, c *git.Client, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(c.Dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "add", "--", name)
	testrepo.Commit(t, c)
}

func mergeBranch(t *testing.T, c *git.Client, branch string, ff bool) {
	t.Helper()
	testrepo.Run(t, c, "checkout", "main")
	mode := "--no-ff"
	if ff {
		mode = "--ff-only"
	}
	testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "merge", mode, "-m", "integrate", branch)
}

func TestSyncMergeFastForwardAndAgain(t *testing.T) {
	for _, ff := range []bool{false, true} {
		t.Run(fmtBool(ff), func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			for round := 0; round < 2; round++ {
				branch := "work-first"
				if round == 1 {
					branch = "work-again"
				}
				started, err := p.Start(ctx, StartOptions{Branch: branch, ID: "task-001", Again: round == 1})
				if err != nil {
					t.Fatal(err)
				}
				if len(started.Attempts) != round+1 {
					t.Fatal("start changed prior history")
				}
				testrepo.Commit(t, c)
				work := syncTask(t, p, started.ID)
				if work.Status != task.Active || work.ActiveAttempt.Observation.WorkCommit == "" || len(work.Attempts) != round+1 {
					t.Fatalf("work observation: %+v", work)
				}
				mergeBranch(t, c, branch, ff)
				gitBefore := testrepo.Run(t, c, "status", "--porcelain=v1", "-z")
				refsBefore := testrepo.Run(t, c, "show-ref")
				plan, err := p.Status(ctx)
				if err != nil {
					t.Fatal(err)
				}
				done, _ := plan.FindID(started.ID)
				kind := task.MergeCommit
				if ff {
					kind = task.FastForward
				}
				if done.Status != task.Done || done.ActiveAttempt != nil || len(done.Attempts) != round+1 || done.Attempts[round].ID != started.ActiveAttempt.ID || done.Attempts[round].Completion.MergeKind != kind || done.Attempts[round].Completion.CompletedAt != nil {
					t.Fatalf("completion: %+v", done)
				}
				if round == 0 && !bytes.Contains(planBytes(t, c), []byte(`"attempts"`)) {
					t.Fatal("first completion missing attempts")
				}
				before := planBytes(t, c)
				backup, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				if err != nil {
					t.Fatal(err)
				}
				if _, changed, err := p.Sync(ctx); err != nil || changed {
					t.Fatalf("repeat: changed=%v err=%v", changed, err)
				}
				afterBackup, _ := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				if !bytes.Equal(before, planBytes(t, c)) || !bytes.Equal(backup, afterBackup) || !bytes.Equal(gitBefore, testrepo.Run(t, c, "status", "--porcelain=v1", "-z")) || !bytes.Equal(refsBefore, testrepo.Run(t, c, "show-ref")) {
					t.Fatal("sync changed no-op data or Git")
				}
			}
		})
	}
}

func TestSyncEmptyUpdatesAndMissedObservations(t *testing.T) {
	for _, scenario := range []string{"empty", "target-fast-forward", "target-merge-wrapper", "all-observations-missed", "attach-integrated"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			if scenario == "attach-integrated" {
				testrepo.Run(t, c, "branch", "work")
				if _, err := p.Attach(ctx, "work", "task-001", false); err != nil {
					t.Fatal(err)
				}
			} else if _, err := p.Start(ctx, StartOptions{Branch: "work"}); err != nil {
				t.Fatal(err)
			}
			before := planBytes(t, c)
			switch scenario {
			case "target-fast-forward", "target-merge-wrapper":
				testrepo.Run(t, c, "checkout", "main")
				testrepo.Commit(t, c)
				testrepo.Run(t, c, "checkout", "work")
				mode := "--ff-only"
				if scenario == "target-merge-wrapper" {
					mode = "--no-ff"
				}
				testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", mode, "-m", "update target", "main")
			case "all-observations-missed":
				testrepo.Commit(t, c)
				mergeBranch(t, c, "work", true)
			}
			item := syncTask(t, p, "task-001")
			if item.Status != task.Active || len(item.Attempts) != 1 || item.ActiveAttempt == nil {
				t.Fatalf("false done: %+v", item)
			}
			if scenario == "all-observations-missed" && (len(item.Warnings) == 0 || item.Warnings[0].Code != "unobserved_work") {
				t.Fatal("missing honest uncertainty")
			}
			if scenario == "empty" && !bytes.Equal(before, planBytes(t, c)) {
				t.Fatal("empty sync rewrote plan")
			}
			after := planBytes(t, c)
			if _, changed, err := p.Sync(ctx); err != nil || changed || !bytes.Equal(after, planBytes(t, c)) {
				t.Fatalf("repeat: %v %v", changed, err)
			}
		})
	}
}

func TestSyncRejectsStaleWorkAndUnsupportedIntegration(t *testing.T) {
	for _, scenario := range []string{"new-tip", "amend", "rebase", "reset-and-return", "delete", "recreate", "expire", "squash", "cherry-pick", "other-target", "target-reset-and-return", "target-missing", "unlogged-ref"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			commitFile(t, c, "work.txt", "own work")
			work := syncTask(t, p, started.ID)
			tip := work.ActiveAttempt.Observation.Tip
			base := started.ActiveAttempt.BaseCommit
			switch scenario {
			case "new-tip":
				testrepo.Commit(t, c)
				testrepo.Run(t, c, "update-ref", "refs/heads/main", tip)
			case "amend":
				testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--amend", "--allow-empty", "-m", "amended")
				mergeBranch(t, c, "work", true)
			case "rebase":
				testrepo.Run(t, c, "checkout", "main")
				commitFile(t, c, "target.txt", "target work")
				testrepo.Run(t, c, "checkout", "work")
				testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "rebase", "main")
				mergeBranch(t, c, "work", true)
			case "reset-and-return":
				testrepo.Run(t, c, "reset", "--hard", base)
				testrepo.Run(t, c, "reset", "--hard", tip)
				mergeBranch(t, c, "work", true)
			case "delete", "recreate":
				mergeBranch(t, c, "work", true)
				testrepo.Run(t, c, "branch", "-D", "work")
				if scenario == "recreate" {
					testrepo.Run(t, c, "branch", "work", tip)
				}
			case "expire":
				mergeBranch(t, c, "work", true)
				testrepo.Run(t, c, "reflog", "expire", "--expire=all", "refs/heads/work")
			case "squash", "cherry-pick":
				testrepo.Run(t, c, "checkout", "main")
				if scenario == "squash" {
					testrepo.Run(t, c, "merge", "--squash", "work")
					testrepo.Commit(t, c)
				} else {
					testrepo.Run(t, c, "-c", "user.name=Cherry", "-c", "user.email=test@example.invalid", "cherry-pick", tip)
				}
			case "other-target":
				testrepo.Run(t, c, "checkout", "-b", "other", "main")
				testrepo.Run(t, c, "merge", "--ff-only", "work")
			case "target-reset-and-return":
				mergeBranch(t, c, "work", true)
				testrepo.Run(t, c, "reset", "--hard", base)
				testrepo.Run(t, c, "reset", "--hard", tip)
			case "target-missing":
				testrepo.Run(t, c, "branch", "-D", "main")
			case "unlogged-ref":
				mergeBranch(t, c, "work", true)
				path := string(bytes.TrimSpace(testrepo.Run(t, c, "rev-parse", "--path-format=absolute", "--git-path", "refs/heads/work")))
				if err := os.WriteFile(path, []byte(base+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			item := syncTask(t, p, started.ID)
			if item.Status != task.Active || len(item.Attempts) != 1 || item.ActiveAttempt.ID != started.ActiveAttempt.ID {
				t.Fatalf("false done: %+v", item)
			}
			if item.ActiveAttempt.Observation.WorkCommit == "" {
				t.Fatal("lost recorded evidence without completion")
			}
		})
	}
}

func TestSyncFreshObservationAfterRewriteAndExplicitRebind(t *testing.T) {
	for _, rebind := range []bool{false, true} {
		t.Run(fmtBool(rebind), func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			testrepo.Commit(t, c)
			syncTask(t, p, started.ID)
			branch := "work"
			if rebind {
				branch = "new-work"
				testrepo.Run(t, c, "branch", "-m", branch)
				if _, err := p.Attach(ctx, branch, started.ID, true); err != nil {
					t.Fatal(err)
				}
				testrepo.Commit(t, c)
			} else {
				testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--amend", "--allow-empty", "-m", "new epoch")
			}
			fresh := syncTask(t, p, started.ID)
			if fresh.ActiveAttempt.Observation.WorkCommit == "" {
				t.Fatal("fresh work not observed")
			}
			mergeBranch(t, c, branch, true)
			if item := syncTask(t, p, started.ID); item.Status != task.Done || item.Attempts[0].ID != started.ActiveAttempt.ID {
				t.Fatalf("fresh completion: %+v", item)
			}
		})
	}
}

func TestSyncCurrentTipEvidenceWithMissedCommitObservation(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	observed := syncTask(t, p, started.ID)
	testrepo.Commit(t, c)
	current, _ := c.BranchCommit(ctx, "work")
	mergeBranch(t, c, "work", true)
	item := syncTask(t, p, started.ID)
	if item.Status != task.Done || item.Attempts[0].Completion.WorkCommit != current || item.Attempts[0].Observation.WorkCommit != observed.ActiveAttempt.Observation.WorkCommit {
		t.Fatalf("current tip or original observation lost: %+v", item)
	}
}

func TestSyncPreservesWorkAcrossIntermediateTargetUpdate(t *testing.T) {
	for _, command := range []string{"sync", "status"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			testrepo.Commit(t, c)
			observed := syncTask(t, p, started.ID)
			proof := *observed.ActiveAttempt.Observation
			mergeBranch(t, c, "work", false)
			testrepo.Run(t, c, "checkout", "work")
			testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", "--no-ff", "-m", "update target", "main")
			before := planBytes(t, c)
			var intermediate task.Task
			if command == "status" {
				plan, err := p.Status(ctx)
				if err != nil {
					t.Fatal(err)
				}
				intermediate, _ = plan.FindID(started.ID)
			} else {
				intermediate = syncTask(t, p, started.ID)
			}
			if intermediate.Status != task.Active || len(intermediate.Attempts) != 1 || len(intermediate.Warnings) != 0 || *intermediate.ActiveAttempt.Observation != proof || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("intermediate update lost proof or wrote no-op: %+v", intermediate)
			}
			current, err := c.BranchCommit(ctx, "work")
			if err != nil {
				t.Fatal(err)
			}
			mergeBranch(t, c, "work", true)
			done := syncTask(t, p, started.ID)
			if done.Status != task.Done || done.ActiveAttempt != nil || len(done.Attempts) != 1 || done.Attempts[0].ID != started.ActiveAttempt.ID || *done.Attempts[0].Observation != proof || done.Attempts[0].Completion.WorkCommit != current || done.Attempts[0].Completion.MergeKind != task.FastForward {
				t.Fatalf("current tip completion: %+v", done)
			}
			before = planBytes(t, c)
			if _, changed, err := p.Sync(ctx); err != nil || changed || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("repeat completion: changed=%v err=%v", changed, err)
			}
		})
	}
}

func TestSyncConflictAndResolvedMerge(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	commitFile(t, c, "shared.txt", "base")
	started, err := p.Start(context.Background(), StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, c, "shared.txt", "work")
	syncTask(t, p, started.ID)
	testrepo.Run(t, c, "checkout", "main")
	commitFile(t, c, "shared.txt", "main")
	if _, err := c.Run(context.Background(), "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", "work"); err == nil {
		t.Fatal("expected conflict")
	}
	item := syncTask(t, p, started.ID)
	if item.Status != task.Active || len(item.Warnings) != 1 || item.Warnings[0].Code != "git_in_progress" {
		t.Fatalf("conflict: %+v", item)
	}
	commitFile(t, c, "shared.txt", "resolved")
	if item = syncTask(t, p, started.ID); item.Status != task.Done {
		t.Fatalf("resolved: %+v", item)
	}
}

func TestSyncPlanConflictCancellationAndGitRace(t *testing.T) {
	for _, scenario := range []string{"plan-conflict", "cancel", "git-race", "write-denied"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			testrepo.Commit(t, c)
			syncTask(t, p, started.ID)
			mergeBranch(t, c, "work", true)
			before := planBytes(t, c)
			var cancel context.CancelFunc
			if scenario == "cancel" {
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
			}
			p.checkpoint = func(point string) error {
				if point != "sync-observed" {
					return nil
				}
				switch scenario {
				case "plan-conflict":
					return os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), append(before, '\n'), 0600)
				case "cancel":
					cancel()
				case "git-race":
					testrepo.Run(t, c, "update-ref", "refs/heads/work", started.ActiveAttempt.BaseCommit)
				case "write-denied":
					return os.WriteFile(filepath.Join(c.Dir, ".git-task", "write.lock"), []byte("other writer"), 0600)
				}
				return nil
			}
			if _, _, err = p.Sync(ctx); err == nil {
				t.Fatal("sync accepted conflict/cancellation")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) || scenario == "plan-conflict" && !errors.Is(err, storage.ErrConflict) || scenario == "write-denied" && !errors.Is(err, storage.ErrLocked) {
				t.Fatalf("wrong error: %v", err)
			}
			var plan task.Plan
			if err := plan.UnmarshalJSON(planBytes(t, c)); err != nil {
				t.Fatal(err)
			}
			item, _ := plan.FindID(started.ID)
			if item.Status != task.Active || item.ActiveAttempt == nil || len(item.Attempts) != 1 {
				t.Fatalf("partial completion: %+v", item)
			}
		})
	}
}

func TestSyncPausedArchivedOrderAndLostCompletion(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	first, err := p.Start(ctx, StartOptions{Branch: "first"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	syncTask(t, p, first.ID)
	second, err := p.Start(ctx, StartOptions{Branch: "second"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	syncTask(t, p, second.ID)
	third, err := p.Start(ctx, StartOptions{Branch: "third"})
	if err != nil {
		t.Fatal(err)
	}
	editPlan(t, p, func(plan *task.Plan) error {
		if _, err := plan.Pause(first.ID); err != nil {
			return err
		}
		_, err := plan.Archive(third.ID)
		return err
	})
	archived := savedTask(t, p, third.ID)
	mergeBranch(t, c, "first", true)
	mergeBranch(t, c, "second", false)
	plan, _, err := p.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := plan.FindID(first.ID)
	b, _ := plan.FindID(second.ID)
	d, _ := plan.FindID(third.ID)
	if a.Status != task.Done || b.Status != task.Done || a.Attempts[0].Completion.Event >= b.Attempts[0].Completion.Event || plan.InsertionTail != b.ID || !reflect.DeepEqual(archived, d) {
		t.Fatalf("order/archive: %+v", plan)
	}
	testrepo.Run(t, c, "reset", "--hard", first.ActiveAttempt.BaseCommit)
	item := syncTask(t, p, first.ID)
	if item.Status != task.Done || item.Warnings[0].Code != "completion_unreachable" || len(item.Attempts) != 1 {
		t.Fatalf("lost completion: %+v", item)
	}
}
