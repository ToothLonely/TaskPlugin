package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover(t *testing.T) {
	c := isolatedClient(t)
	root := c.Dir
	nested := filepath.Join(root, "вложенная папка")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, nested} {
		c.Dir = dir
		repo, err := c.Discover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][2]string{
			{repo.Root, root}, {repo.GitDir, filepath.Join(root, ".git")},
			{repo.CommonDir, filepath.Join(root, ".git")}, {repo.ExcludePath, filepath.Join(root, ".git", "info", "exclude")},
		} {
			samePath(t, pair[0], pair[1])
		}
	}
	// Discovery also works once HEAD exists and then becomes detached.
	c.Dir = root
	mustRun(t, c, "commit", "--allow-empty", "-m", "initial")
	mustRun(t, c, "checkout", "--detach")
	if _, err := c.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverOutsideAndBare(t *testing.T) {
	c := isolatedClient(t)
	c.Dir = t.TempDir()
	if _, err := c.Discover(context.Background()); err == nil {
		t.Fatal("outside repository accepted")
	} else {
		var command *CommandError
		if !errors.As(err, &command) {
			t.Fatalf("Git error lost: %v", err)
		}
	}
	mustRun(t, c, "init", "--bare", "--template=")
	if _, err := c.Discover(context.Background()); !errors.Is(err, ErrNotWorkTree) {
		t.Fatalf("bare error=%v", err)
	}
}

func TestDiscoverSeparateGitDirectory(t *testing.T) {
	c := isolatedClient(t)
	base := t.TempDir()
	gitDir := filepath.Join(base, "отдельный git directory")
	c.Dir = base
	mustRun(t, c, "init", "--initial-branch=main", "--template=", "--separate-git-dir", gitDir)
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	samePath(t, repo.Root, base)
	samePath(t, repo.GitDir, gitDir)
	samePath(t, repo.CommonDir, gitDir)
	samePath(t, repo.ExcludePath, filepath.Join(gitDir, "info", "exclude"))
}

func TestDiscoverLinkedWorktreePaths(t *testing.T) {
	c := isolatedClient(t)
	root := c.Dir
	mustRun(t, c, "commit", "--allow-empty", "-m", "initial")
	linked := filepath.Join(t.TempDir(), "linked tree")
	mustRun(t, c, "worktree", "add", "--detach", linked)
	c.Dir = linked
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	samePath(t, repo.Root, linked)
	samePath(t, repo.CommonDir, filepath.Join(root, ".git"))
	samePath(t, repo.ExcludePath, filepath.Join(root, ".git", "info", "exclude"))
	if repo.GitDir == repo.CommonDir {
		t.Fatal("linked worktree directory confused with common directory")
	}
	// This is a path-discovery test, not support for plan operations in worktrees.
}

func samePath(t *testing.T, got, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if errors.Is(err, os.ErrNotExist) {
		if _, wantErr := os.Stat(want); !errors.Is(wantErr, os.ErrNotExist) || filepath.Base(got) != filepath.Base(want) {
			t.Fatalf("path=%q, want %q", got, want)
		}
		samePath(t, filepath.Dir(got), filepath.Dir(want))
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("path=%q, want %q", got, want)
	}
}
