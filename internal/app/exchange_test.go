package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-task/internal/git"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func exchangePending(t *testing.T, p *Plans) []task.Action {
	t.Helper()
	s, err := p.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s.Plan.Team.Pending
}

func TestExchangeDoesNotCompleteEmptyOrSquashedWork(t *testing.T) {
	for _, mode := range []string{"empty", "squash"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			started, err := f.a.Start(ctx, StartOptions{Branch: "work", ID: f.id})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "squash" {
				commitFile(t, f.ca, "work.txt", "work")
				if _, _, err := f.a.Sync(ctx); err != nil {
					t.Fatal(err)
				}
			}
			testrepo.Run(t, f.ca, "checkout", "main")
			if mode == "squash" {
				commitFile(t, f.ca, "target.txt", "different parent for squash")
				testrepo.Run(t, f.ca, "merge", "--squash", "work")
				testrepo.Commit(t, f.ca)
			}
			if err := f.a.Push(ctx); err != nil {
				t.Fatal(err)
			}
			if err := f.b.Pull(ctx, ""); err != nil {
				t.Fatal(err)
			}
			for _, p := range []*Plans{f.a, f.b} {
				item, _, err := p.Show(ctx, f.id)
				if err != nil || item.Status != task.Active || len(item.Attempts) != 1 || item.Attempts[0].ID != started.ActiveAttempt.ID {
					t.Fatalf("false completion or lost history: %+v %v", item, err)
				}
			}
		})
	}
}

func TestExchangePullProtectsLocalStorage(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	testrepo.Run(t, f.ca, "add", "--force", ".git-task/plan.json")
	testrepo.Commit(t, f.ca)
	teamNetwork(t, f.ca, "push", "origin", "refs/heads/main:refs/heads/main")
	before := planBytes(t, f.cb)
	head := testrepo.Run(t, f.cb, "rev-parse", "HEAD")
	if err := f.b.Pull(ctx, ""); err == nil {
		t.Fatal("pull accepted tracked storage")
	}
	if !bytes.Equal(before, planBytes(t, f.cb)) || !bytes.Equal(head, testrepo.Run(t, f.cb, "rev-parse", "HEAD")) {
		t.Fatal("pull overwrote local storage")
	}
}

func TestExchangeRaceAndConflictingQueue(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "race", true: "conflict"}[conflict], func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			if conflict {
				for p, title := range map[*Plans]string{f.a: "Alice", f.b: "Bob"} {
					preview, err := p.PrepareEdit(ctx, f.id)
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := p.ApplyEdit(ctx, preview, task.EditOptions{Title: &title}); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if _, err := f.a.Add(ctx, "Alice", "", task.Position{}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.b.Add(ctx, "Bob", "", task.Position{}); err != nil {
					t.Fatal(err)
				}
			}
			pending := exchangePending(t, f.a)
			fired := false
			f.a.checkpoint = func(name string) error {
				if name == "publish-before-push" && !fired {
					fired = true
					return f.b.Publish(ctx)
				}
				return nil
			}
			err := f.a.Push(ctx)
			if !fired {
				t.Fatal("race checkpoint not reached")
			}
			if conflict {
				if !errors.Is(err, task.ErrSharedConflict) {
					t.Fatalf("conflict lost: %v", err)
				}
				if !reflect.DeepEqual(pending, exchangePending(t, f.a)) {
					t.Fatal("conflicting queue lost")
				}
				item, _, err := f.a.Show(ctx, f.id)
				if err != nil || item.Title != "Alice" {
					t.Fatalf("local changed: %+v %v", item, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.b.Pull(ctx, ""); err != nil {
				t.Fatal(err)
			}
			s, err := f.b.store.Load(ctx)
			if err != nil || len(s.Plan.Tasks) != 3 || len(exchangePending(t, f.a)) != 0 {
				t.Fatalf("race lost work: %+v %v", s.Plan, err)
			}
		})
	}
}

func TestExchangePushUsesOnlyExplicitUpstream(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	testrepo.Run(t, f.ca, "checkout", "-b", "feature")
	testrepo.Run(t, f.ca, "config", "branch.feature.remote", "origin")
	testrepo.Run(t, f.ca, "config", "branch.feature.merge", "refs/heads/destination")
	commitFile(t, f.ca, "feature.txt", "feature")
	testrepo.Run(t, f.ca, "tag", "-a", "private-tag", "-m", "private")
	testrepo.Run(t, f.ca, "config", "push.followTags", "true")
	testrepo.Run(t, f.ca, "config", "remote.origin.push", "refs/heads/main:refs/heads/unwanted")
	if _, err := f.a.Add(ctx, "publish plan too", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Push(ctx); err != nil {
		t.Fatal(err)
	}
	refs := string(teamNetwork(t, f.ca, "ls-remote", "--refs", "origin"))
	if !strings.Contains(refs, "refs/heads/destination") || strings.Contains(refs, "refs/tags/") || strings.Contains(refs, "refs/heads/unwanted") {
		t.Fatal(refs)
	}
	if got := strings.TrimSpace(string(testrepo.Run(t, f.ca, "config", "branch.feature.merge"))); got != "refs/heads/destination" {
		t.Fatal("upstream changed", got)
	}
}

func TestExchangePushPullWithoutHooks(t *testing.T) {
	for _, ff := range []bool{false, true} {
		t.Run(map[bool]string{false: "merge", true: "ff"}[ff], func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			started, err := f.a.Start(ctx, StartOptions{Branch: "work", ID: f.id})
			if err != nil {
				t.Fatal(err)
			}
			commitFile(t, f.ca, "work.txt", "work\n")
			if _, _, err := f.a.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			mergeBranch(t, f.ca, "work", ff)
			if err := f.a.Push(ctx); err != nil {
				t.Fatal(err)
			}
			if len(exchangePending(t, f.a)) != 0 {
				t.Fatal("push left pending actions")
			}
			testrepo.Run(t, f.cb, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
			local, err := f.b.Add(ctx, "offline local", "", task.Position{})
			if err != nil {
				t.Fatal(err)
			}
			pending := exchangePending(t, f.b)
			if err := f.b.Pull(ctx, ""); err != nil {
				t.Fatal(err)
			}
			got, _, err := f.b.Show(ctx, f.id)
			if err != nil || got.Status != task.Done || len(got.Attempts) != 1 || got.Attempts[0].ID != started.ActiveAttempt.ID {
				t.Fatalf("received completion: %+v %v", got, err)
			}
			if _, _, err := f.b.Show(ctx, local.ID); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(exchangePending(t, f.b), pending) {
				t.Fatal("pull changed local action IDs")
			}
			if !bytes.Equal(testrepo.Run(t, f.ca, "rev-parse", "HEAD"), testrepo.Run(t, f.cb, "rev-parse", "HEAD")) {
				t.Fatal("code not received")
			}
			if paths := testrepo.Run(t, f.cb, "ls-files", ".git-task", "plan.json"); len(paths) != 0 {
				t.Fatalf("plan tracked: %s", paths)
			}
			if err := f.a.FetchTeam(ctx); err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.a.Show(ctx, local.ID); !errors.Is(err, task.ErrNotFound) {
				t.Fatalf("pull published queue: %v", err)
			}
			if err := f.b.Pull(ctx, ""); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(exchangePending(t, f.b), pending) {
				t.Fatal("repeat duplicated queue")
			}
		})
	}
}

func TestExchangePullPlanOnlyAndIntegrationModes(t *testing.T) {
	for _, mode := range []string{"plan-only", "merge", "rebase", "ff-rejected", "merge-conflict", "rebase-conflict"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			before := testrepo.Run(t, f.cb, "rev-parse", "HEAD")
			if mode != "plan-only" {
				commitFile(t, f.ca, "remote.txt", "remote\n")
				name := "local.txt"
				if strings.HasSuffix(mode, "conflict") {
					name = "remote.txt"
				}
				commitFile(t, f.cb, name, "local\n")
			}
			added, err := f.a.Add(ctx, mode, "", task.Position{})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.a.Push(ctx); err != nil {
				t.Fatal(err)
			}
			testrepo.Run(t, f.cb, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
			testrepo.Run(t, f.cb, "config", "pull.ff", "only")
			testrepo.Run(t, f.cb, "config", "pull.rebase", "true")
			pullMode := strings.TrimSuffix(mode, "-conflict")
			if mode == "plan-only" || mode == "ff-rejected" {
				pullMode = "ff-only"
			}
			err = f.b.Pull(ctx, pullMode)
			if mode == "ff-rejected" || strings.HasSuffix(mode, "conflict") {
				if err == nil {
					t.Fatal("expected integration failure")
				}
				if _, _, e := f.b.Show(ctx, added.ID); !errors.Is(e, task.ErrNotFound) {
					t.Fatalf("plan fetched after conflict: %v", e)
				}
				if strings.HasSuffix(mode, "conflict") && !errors.Is(f.cb.CheckStart(ctx, false), git.ErrInProgress) {
					t.Fatal("conflict state lost")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.b.Show(ctx, added.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "plan-only" && !bytes.Equal(before, testrepo.Run(t, f.cb, "rev-parse", "HEAD")) {
				t.Fatal("plan-only changed HEAD")
			}
			if mode != "plan-only" {
				for _, path := range []string{"local.txt", "remote.txt"} {
					if _, err := os.Stat(filepath.Join(f.cb.Dir, path)); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestExchangeRejectedCodeDoesNotPublish(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	commitFile(t, f.ca, "a.txt", "a")
	if err := f.a.Push(ctx); err != nil {
		t.Fatal(err)
	}
	commitFile(t, f.cb, "b.txt", "b")
	added, err := f.b.Add(ctx, "not sent", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	pending := exchangePending(t, f.b)
	if err := f.b.Push(ctx); err == nil {
		t.Fatal("expected non-fast-forward rejection")
	}
	if !reflect.DeepEqual(pending, exchangePending(t, f.b)) {
		t.Fatal("queue changed")
	}
	if err := f.a.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.a.Show(ctx, added.ID); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("plan published after code rejection: %v", err)
	}
}

func TestExchangePartialResultsAndCancellation(t *testing.T) {
	for _, mode := range []string{"push-offline", "pull-offline", "push-cancel", "pull-cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, c := f.a, f.ca
			commitFile(t, f.ca, "new.txt", "new")
			if strings.HasPrefix(mode, "pull") {
				if err := f.a.Push(ctx); err != nil {
					t.Fatal(err)
				}
				p, c = f.b, f.cb
			}
			if _, err := p.Add(ctx, "pending", "", task.Position{}); err != nil {
				t.Fatal(err)
			}
			pending := exchangePending(t, p)
			p.checkpoint = func(name string) error {
				if name != "exchange-code-pushed" && name != "exchange-code-pulled" {
					return nil
				}
				if strings.HasSuffix(mode, "cancel") {
					cancel()
					return ctx.Err()
				}
				testrepo.Run(t, c, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
				return nil
			}
			var err error
			if strings.HasPrefix(mode, "push") {
				err = p.Push(ctx)
			} else {
				err = p.Pull(ctx, "")
			}
			if err == nil || !strings.Contains(err.Error(), "код ") {
				t.Fatalf("partial result missing: %v", err)
			}
			if strings.HasSuffix(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if !reflect.DeepEqual(pending, exchangePending(t, p)) {
				t.Fatal("queue lost")
			}
			p.checkpoint = nil
			testrepo.Run(t, c, "remote", "set-url", "origin", f.remote)
			if err := p.Publish(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := p.Publish(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(exchangePending(t, p)) != 0 {
				t.Fatal("repeat publication did not acknowledge queue")
			}
		})
	}
}

func TestExchangePreconditionsDoNotMutate(t *testing.T) {
	for _, mode := range []string{"no-upstream", "different-remote", "push-remote", "push-url", "many-urls", "mirror", "detached", "plan-upstream", "in-progress", "dirty-pull", "no-team"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			switch mode {
			case "no-upstream":
				testrepo.Run(t, f.ca, "config", "--unset", "branch.main.merge")
			case "different-remote":
				testrepo.Run(t, f.ca, "config", "branch.main.remote", "other")
			case "push-remote":
				testrepo.Run(t, f.ca, "config", "branch.main.pushRemote", "other")
			case "push-url":
				testrepo.Run(t, f.ca, "config", "remote.origin.pushurl", filepath.Join(t.TempDir(), "other.git"))
			case "many-urls":
				testrepo.Run(t, f.ca, "config", "--add", "remote.origin.url", f.remote)
			case "mirror":
				testrepo.Run(t, f.ca, "config", "remote.origin.mirror", "true")
			case "detached":
				testrepo.Run(t, f.ca, "checkout", "--detach")
			case "plan-upstream":
				testrepo.Run(t, f.ca, "config", "branch.main.merge", "refs/heads/git-task-plan")
			case "in-progress":
				if err := os.WriteFile(filepath.Join(f.ca.Dir, ".git", "MERGE_HEAD"), testrepo.Run(t, f.ca, "rev-parse", "HEAD"), 0600); err != nil {
					t.Fatal(err)
				}
			case "dirty-pull":
				if err := os.WriteFile(filepath.Join(f.ca.Dir, "dirty.txt"), []byte("dirty"), 0600); err != nil {
					t.Fatal(err)
				}
			case "no-team":
				f.a, f.ca = f.clone("Local")
			}
			before := planBytes(t, f.ca)
			head := testrepo.Run(t, f.ca, "rev-parse", "HEAD")
			if err := f.a.Pull(ctx, ""); err == nil {
				t.Fatal("pull accepted invalid precondition")
			}
			if mode != "dirty-pull" {
				if err := f.a.Push(ctx); err == nil {
					t.Fatal("push accepted invalid precondition")
				}
			}
			if !bytes.Equal(before, planBytes(t, f.ca)) || !bytes.Equal(head, testrepo.Run(t, f.ca, "rev-parse", "HEAD")) {
				t.Fatal("precondition changed plan/code")
			}
		})
	}
}
