package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestResumePreservesAttemptAndBinding(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	if _, _, err := p.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	paused, _, err := p.Pause(ctx, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "checkout", "main")
	resumed, err := p.Resume(ctx, started.ID)
	if err != nil || resumed.Status != task.Active || !sameAttemptBinding(resumed.ActiveAttempt, paused.ActiveAttempt) || !sameAttemptBindings(resumed.Attempts, paused.Attempts) {
		t.Fatalf("resume changed attempt/history: %+v %v", resumed, err)
	}
	head, err := c.HeadState(ctx)
	if err != nil || head.Ref != "refs/heads/work" || head.Commit != paused.ActiveAttempt.Observation.Tip {
		t.Fatalf("wrong checkout: %+v %v", head, err)
	}
	before := planBytes(t, c)
	if _, err := p.Resume(ctx, started.ID); !errors.Is(err, task.ErrTransition) || !strings.Contains(err.Error(), "уже запущена") || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("repeat active resume: %v", err)
	}
	if _, _, err := p.Pause(ctx, started.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Resume(ctx, started.ID); err != nil {
		t.Fatalf("resume already checked-out paused branch: %v", err)
	}
}

func TestResumeMissingRenamedAndRecreatedBranch(t *testing.T) {
	for _, mode := range []string{"renamed", "missing", "recreated"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := p.Pause(ctx, started.ID); err != nil {
				t.Fatal(err)
			}
			testrepo.Run(t, c, "checkout", "main")
			if mode == "renamed" {
				testrepo.Run(t, c, "branch", "-m", "work", "renamed")
			} else {
				testrepo.Run(t, c, "branch", "-D", "work")
				if mode == "recreated" {
					testrepo.Run(t, c, "branch", "work")
				}
			}
			before := planBytes(t, c)
			head, _ := c.HeadState(ctx)
			if _, err := p.Resume(ctx, started.ID); err == nil || !strings.Contains(err.Error(), "--rebind") || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("unconfirmed binding resumed: %v", err)
			}
			if after, _ := c.HeadState(ctx); after != head {
				t.Fatal("failure switched HEAD")
			}
			if mode == "missing" {
				return
			}
			branch := "work"
			if mode == "renamed" {
				branch = "renamed"
			}
			rebound, err := p.Attach(ctx, branch, started.ID, true)
			if err != nil || len(rebound.ActiveAttempt.Rebindings) != 1 || rebound.ActiveAttempt.ID != started.ActiveAttempt.ID {
				t.Fatalf("explicit rebind: %+v %v", rebound, err)
			}
			if _, err := p.Resume(ctx, started.ID); err != nil {
				t.Fatalf("resume rebound: %v", err)
			}
		})
	}
}

func TestResumeRecoveryAtBoundaries(t *testing.T) {
	for _, boundary := range []string{"prepared", "checked-out", "committed"} {
		for _, foreign := range []bool{false, true} {
			t.Run(boundary+fmtBool(foreign), func(t *testing.T) {
				p, c := startFixture(t)
				ctx := context.Background()
				started, err := p.Start(ctx, StartOptions{Branch: "work"})
				if err != nil {
					t.Fatal(err)
				}
				paused, _, err := p.Pause(ctx, started.ID)
				if err != nil {
					t.Fatal(err)
				}
				testrepo.Run(t, c, "checkout", "main")
				before := planBytes(t, c)
				p.checkpoint = func(name string) error {
					if name == boundary {
						return errors.New("interrupted")
					}
					return nil
				}
				if _, err := p.Resume(ctx, started.ID); err == nil {
					t.Fatal("injected failure ignored")
				}
				p.checkpoint = nil
				if boundary != "committed" && !bytes.Equal(before, planBytes(t, c)) {
					t.Fatal("failed resume partially saved plan")
				}
				if foreign {
					data := append(planBytes(t, c), '\n')
					if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
						t.Fatal(err)
					}
					if recovered, err := p.RecoverStart(ctx); !errors.Is(err, storage.ErrConflict) || recovered || !bytes.Equal(data, planBytes(t, c)) {
						t.Fatalf("foreign edit overwritten: %v %v", recovered, err)
					}
					return
				}
				if recovered, err := p.RecoverStart(ctx); err != nil || !recovered {
					t.Fatalf("recover: %v %v", recovered, err)
				}
				want := task.Active
				if boundary == "prepared" {
					want = task.Paused
				}
				item := savedTask(t, p, started.ID)
				if item.Status != want || !sameAttemptBinding(item.ActiveAttempt, paused.ActiveAttempt) || len(item.Attempts) != 1 {
					t.Fatalf("recovery changed attempt: %+v", item)
				}
				installed := planBytes(t, c)
				head, err := c.HeadState(ctx)
				wantRef := "refs/heads/work"
				if boundary == "prepared" {
					wantRef = "refs/heads/main"
				}
				if err != nil || head.Ref != wantRef {
					t.Fatalf("recovery switched HEAD: %+v %v", head, err)
				}
				backup, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				if err != nil {
					t.Fatal(err)
				}
				if recovered, err := p.RecoverStart(ctx); err != nil || recovered || !bytes.Equal(installed, planBytes(t, c)) {
					t.Fatalf("repeat recover: %v %v", recovered, err)
				}
				backupAfter, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				if err != nil || !bytes.Equal(backup, backupAfter) {
					t.Fatalf("repeat recover changed backup: %v", err)
				}
			})
		}
	}
}

func TestResumeHookFailureAfterSwitch(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Pause(ctx, started.ID); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "checkout", "main")
	hooks := strings.TrimSpace(string(testrepo.Run(t, c, "config", "--get", "core.hooksPath")))
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	resumed, err := p.Resume(ctx, started.ID)
	if err == nil || !strings.Contains(err.Error(), "уже продолжена") || resumed.Status != task.Active || savedTask(t, p, started.ID).Status != task.Active {
		t.Fatalf("hook failure lost actual switch: %+v %v", resumed, err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "operation.json")); !os.IsNotExist(err) {
		t.Fatalf("successful switch left journal: %v", err)
	}
}

func TestResumeRefusesUnsafeStateBeforeJournal(t *testing.T) {
	for _, mode := range []string{"detached", "dirty", "operation", "target-missing"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := p.Pause(ctx, started.ID); err != nil {
				t.Fatal(err)
			}
			testrepo.Run(t, c, "checkout", "main")
			switch mode {
			case "detached":
				testrepo.Run(t, c, "checkout", "--detach", "main")
			case "dirty":
				if err := os.WriteFile(filepath.Join(c.Dir, "untracked"), []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			case "operation":
				repo, err := c.Discover(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(repo.GitDir, "rebase-merge"), 0700); err != nil {
					t.Fatal(err)
				}
			case "target-missing":
				testrepo.Run(t, c, "checkout", "work")
				testrepo.Run(t, c, "branch", "-D", "main")
			}
			head, err := c.HeadState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			before := planBytes(t, c)
			if _, err := p.Resume(ctx, started.ID); err == nil || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("unsafe state accepted: %v", err)
			}
			if after, err := c.HeadState(ctx); err != nil || after != head {
				t.Fatalf("refusal changed HEAD: %+v %v", after, err)
			}
			if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "operation.json")); !os.IsNotExist(err) {
				t.Fatalf("refusal left journal: %v", err)
			}
		})
	}
}

func TestResumeCancellationAndForeignGitEffect(t *testing.T) {
	for _, mode := range []string{"cancel", "foreign-checkout", "branch-change", "target-change"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			started, err := p.Start(ctx, StartOptions{Branch: "work"})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := p.Pause(ctx, started.ID); err != nil {
				t.Fatal(err)
			}
			testrepo.Run(t, c, "checkout", "main")
			before := planBytes(t, c)
			p.checkpoint = func(name string) error {
				if name == "checked-out" {
					return context.Canceled
				}
				return nil
			}
			if _, err := p.Resume(ctx, started.ID); !errors.Is(err, context.Canceled) || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("cancellation lost source: %v", err)
			}
			p.checkpoint = nil
			switch mode {
			case "foreign-checkout":
				testrepo.Run(t, c, "checkout", "main")
				testrepo.Run(t, c, "checkout", "work")
			case "branch-change":
				testrepo.Commit(t, c)
			case "target-change":
				testrepo.Run(t, c, "checkout", "main")
				testrepo.Commit(t, c)
				testrepo.Run(t, c, "checkout", "work")
			}
			if mode == "cancel" {
				if recovered, err := p.RecoverStart(ctx); err != nil || !recovered || savedTask(t, p, started.ID).Status != task.Active {
					t.Fatalf("cancel recovery: %v %v", recovered, err)
				}
				return
			}
			head, err := c.HeadState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if recovered, err := p.RecoverStart(ctx); err == nil || recovered || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("foreign effect accepted: %v %v", recovered, err)
			}
			if after, err := c.HeadState(ctx); err != nil || after != head {
				t.Fatalf("refused recovery changed Git: %+v %v", after, err)
			}
		})
	}
}
