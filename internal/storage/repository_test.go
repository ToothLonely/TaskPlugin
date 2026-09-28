package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"git-task/internal/git"
	"git-task/internal/testrepo"
)

func TestUnsupportedRepositoriesRefuseStorage(t *testing.T) {
	for _, kind := range []string{"shallow", "partial", "worktree-main", "worktree-linked", "bare"} {
		t.Run(kind, func(t *testing.T) {
			s, c := fixture(t)
			testrepo.Commit(t, c)
			switch kind {
			case "shallow":
				head := testrepo.Run(t, c, "rev-parse", "HEAD")
				put(t, filepath.Join(c.Dir, ".git", "shallow"), head)
			case "partial":
				testrepo.Run(t, c, "config", "--local", "remote.origin.promisor", "true")
			case "worktree-main", "worktree-linked":
				other := filepath.Join(filepath.Dir(c.Dir), "linked")
				testrepo.Run(t, c, "worktree", "add", "-b", "linked", other)
				if kind == "worktree-linked" {
					linked, err := git.New(other)
					if err != nil {
						t.Fatal(err)
					}
					linked.Env = c.Env
					repo, err := linked.Discover(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					s = New(repo.Root, repo.ExcludePath, linked)
				}
			case "bare":
				testrepo.Run(t, c, "config", "--local", "core.bare", "true")
			}
			if _, err := s.Init(context.Background(), "main"); err == nil {
				t.Fatal("unsupported repository accepted")
			}
			if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
				t.Fatalf("storage created in unsupported repository: %v", err)
			}
		})
	}
}

func TestDisabledPromisorIsNotPartialClone(t *testing.T) {
	s, c := fixture(t)
	testrepo.Run(t, c, "config", "--local", "remote.origin.promisor", "false")
	if created, err := s.Init(context.Background(), "main"); err != nil || !created {
		t.Fatalf("ordinary repository rejected: %v %v", created, err)
	}
}

func TestSymlinkStoragePreservesDestination(t *testing.T) {
	s, _ := fixture(t)
	destination := filepath.Join(filepath.Dir(s.dir), "outside-data")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "keep")
	put(t, sentinel, []byte("keep"))
	if err := os.Symlink(destination, s.dir); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v; junction is tested separately", err)
		}
		t.Fatal(err)
	}
	if _, err := s.Init(context.Background(), "main"); err == nil {
		t.Fatal("symlink accepted")
	}
	if string(read(t, sentinel)) != "keep" {
		t.Fatal("destination touched")
	}
}

func TestInitInterruptedAfterExcludeCanRetry(t *testing.T) {
	s, c := fixture(t)
	// The exclusion is installed, but a higher-priority negation prevents the
	// plan publication. Removing the conflict lets the next init safely continue.
	ignore := filepath.Join(c.Dir, ".gitignore")
	put(t, ignore, []byte("!/.git-task/\n"))
	if _, err := s.Init(context.Background(), "main"); err == nil {
		t.Fatal("expected conflict")
	}
	exclusion := read(t, s.excludePath)
	if bytes.Count(exclusion, []byte("/.git-task/")) != 1 {
		t.Fatalf("exclude=%q", exclusion)
	}
	put(t, ignore, []byte("# conflict resolved\n"))
	if created, err := s.Init(context.Background(), "main"); err != nil || !created {
		t.Fatalf("retry: %v %v", created, err)
	}
	if !bytes.Equal(exclusion, read(t, s.excludePath)) {
		t.Fatal("retry duplicated exclusion")
	}
}

func TestCorruptedPlanIsNotRestoredFromBackup(t *testing.T) {
	s, _ := initialized(t)
	snap := snapshot(t, s)
	if _, err := s.Save(context.Background(), snap, added(t, snap, "task")); err != nil {
		t.Fatal(err)
	}
	backup := read(t, filepath.Join(s.dir, "plan.backup.json"))
	path := filepath.Join(s.dir, "plan.json")
	put(t, path, []byte("broken"))
	if _, err := s.Load(context.Background()); err == nil {
		t.Fatal("corruption hidden")
	}
	if _, err := s.Init(context.Background(), "main"); err == nil {
		t.Fatal("init reset corrupted plan")
	}
	if string(read(t, path)) != "broken" || !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
		t.Fatal("corrupted plan or backup overwritten")
	}
}

func TestWhitespaceNoOpAndBackupNoOp(t *testing.T) {
	s, _ := initialized(t)
	snap := snapshot(t, s)
	if _, err := s.Save(context.Background(), snap, added(t, snap, "task")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, "plan.json")
	data := append(read(t, path), []byte(" \r\n")...)
	put(t, path, data)
	backup := read(t, filepath.Join(s.dir, "plan.backup.json"))
	snap = snapshot(t, s)
	if changed, err := s.Save(context.Background(), snap, snap.Plan); err != nil || changed {
		t.Fatalf("no-op: %v %v", changed, err)
	}
	if !bytes.Equal(data, read(t, path)) || !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
		t.Fatal("no-op changed bytes")
	}
}
