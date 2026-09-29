package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/testrepo"
)

func TestReviewStartRejectsDetachedHEAD(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	testrepo.Run(t, c, "checkout", "--detach", "main")
	before := planBytes(t, c)
	head, err := c.HeadState(ctx)
	if err != nil || head.Ref != "" {
		t.Fatalf("detached fixture: %+v %v", head, err)
	}
	_, startErr := p.Start(ctx, StartOptions{Branch: "detached-start"})
	if startErr == nil {
		t.Error("start accepted detached HEAD; SPEC requires refusal before mutation")
	}
	after, err := c.HeadState(ctx)
	if err != nil || after != head {
		t.Errorf("HEAD changed: before=%+v after=%+v err=%v", head, after, err)
	}
	if !bytes.Equal(before, planBytes(t, c)) {
		t.Error("detached start changed plan")
	}
	branch, err := c.BranchCommit(ctx, "detached-start")
	if err != nil || branch != "" {
		t.Errorf("detached start created branch: %q %v", branch, err)
	}
}

func TestReviewAttachRejectsUnmergedIndex(t *testing.T) {
	t.Parallel()
	for _, rebind := range []bool{false, true} {
		t.Run(fmtBool(rebind), func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			testrepo.Run(t, c, "branch", "existing")
			if rebind {
				if _, err := p.Attach(ctx, "existing", "task-001", false); err != nil {
					t.Fatal(err)
				}
			}
			testrepo.Run(t, c, "branch", "replacement")
			file := filepath.Join(c.Dir, "conflict.txt")
			writeCommit := func(value string) {
				t.Helper()
				if err := os.WriteFile(file, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
				testrepo.Run(t, c, "add", "conflict.txt")
				testrepo.Commit(t, c)
			}
			writeCommit("base\n")
			testrepo.Run(t, c, "checkout", "-b", "side")
			writeCommit("side\n")
			testrepo.Run(t, c, "checkout", "main")
			writeCommit("main\n")
			if _, err := c.Run(ctx, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", "side"); err == nil {
				t.Fatal("fixture merge did not conflict")
			}
			testrepo.Run(t, c, "merge", "--quit")
			index := testrepo.Run(t, c, "ls-files", "--unmerged", "-z")
			if len(index) == 0 {
				t.Fatal("fixture has no unmerged entries")
			}
			before := planBytes(t, c)
			if _, err := p.Attach(ctx, "replacement", "task-001", rebind); err == nil {
				t.Error("attach/rebind accepted unmerged index; SPEC requires refusal")
			}
			if !bytes.Equal(before, planBytes(t, c)) {
				t.Error("attach/rebind changed plan with unmerged index")
			}
			if !bytes.Equal(index, testrepo.Run(t, c, "ls-files", "--unmerged", "-z")) {
				t.Error("attach/rebind changed conflict entries")
			}
		})
	}
}
