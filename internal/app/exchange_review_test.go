package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestExchangeReviewRebasePreservesPlanAcrossHistoricalStorage(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	testrepo.Run(t, f.cb, "add", "--force", ".git-task/plan.json")
	testrepo.Commit(t, f.cb)
	testrepo.Run(t, f.cb, "rm", "--cached", ".git-task/plan.json")
	testrepo.Commit(t, f.cb)
	if _, err := f.b.Add(ctx, "unpublished local work", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, f.ca, "remote.txt", "remote\n")
	teamNetwork(t, f.ca, "push", "origin", "refs/heads/main:refs/heads/main")
	before := planBytes(t, f.cb)
	head := testrepo.Run(t, f.cb, "rev-parse", "HEAD")
	index := testrepo.Run(t, f.cb, "ls-files", "--stage", "-z")
	err := f.b.Pull(ctx, "rebase")
	if err == nil {
		t.Fatal("unsafe historical storage accepted for rebase")
	}
	after, readErr := os.ReadFile(filepath.Join(f.cb.Dir, ".git-task", "plan.json"))
	if readErr != nil {
		t.Fatalf("local plan lost during rebase: %v", readErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rebase changed local plan while replaying historical tracked storage")
	}
	if !bytes.Equal(head, testrepo.Run(t, f.cb, "rev-parse", "HEAD")) || !bytes.Equal(index, testrepo.Run(t, f.cb, "ls-files", "--stage", "-z")) {
		t.Fatal("unsafe rebase changed HEAD or index")
	}
	if err := f.cb.CheckStart(ctx, true); err != nil {
		t.Fatalf("unsafe rebase left an operation or changed worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.cb.Dir, "remote.txt")); !os.IsNotExist(err) {
		t.Fatalf("incoming code integrated despite unsafe history: %v", err)
	}
}

func TestExchangeReviewUsesNamedTeamRemoteWithAnotherOrigin(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	p, c := f.clone("Third")
	testrepo.Run(t, c, "remote", "rename", "origin", "team")
	other := filepath.Join(t.TempDir(), "other.git")
	testrepo.Run(t, c, "init", "--bare", "--initial-branch=main", other)
	testrepo.Run(t, c, "remote", "add", "origin", other)
	if err := p.Connect(ctx, "team"); err != nil {
		t.Fatal(err)
	}
	item, err := p.Add(ctx, "named remote work", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, c, "third.txt", "third\n")
	if err := p.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if refs := teamNetwork(t, c, "ls-remote", "--refs", "origin"); len(refs) != 0 {
		t.Fatalf("unrelated origin modified: %s", refs)
	}
	if err := f.b.Pull(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.b.Show(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if got := string(testrepo.Run(t, f.cb, "show", "HEAD:third.txt")); got != "third\n" {
		t.Fatal(got)
	}
	if remote := strings.TrimSpace(string(testrepo.Run(t, c, "config", "branch.main.remote"))); remote != "team" {
		t.Fatal("upstream changed", remote)
	}
}
