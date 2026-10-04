// Package testrepo supplies isolated real Git repositories for integration tests.
package testrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/git"
)

// New creates an unborn repository without a configured Git identity.
func New(t *testing.T) *git.Client {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "репозиторий worktree с пробелами")
	for _, p := range []string{root, filepath.Join(base, "home"), filepath.Join(base, "hooks"), filepath.Join(base, "template")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	c, err := git.New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "GIT_") || key == "HOME" || key == "USERPROFILE" || key == "XDG_CONFIG_HOME" {
			continue
		}
		c.Env = append(c.Env, entry)
	}
	home := filepath.Join(base, "home")
	c.Env = append(c.Env, "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+home,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_ATTR_NOSYSTEM=1",
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(base))
	Run(t, c, "init", "--initial-branch=main", "--template="+filepath.Join(base, "template"))
	Run(t, c, "config", "--local", "core.hooksPath", filepath.Join(base, "hooks"))
	Run(t, c, "config", "--local", "core.autocrlf", "false")
	return c
}

// Run fails a test on any unexpected Git error.
func Run(t *testing.T, c *git.Client, args ...string) []byte {
	t.Helper()
	r, err := c.Run(context.Background(), args...)
	if err != nil {
		t.Fatal(err)
	}
	return r.Stdout
}

// Commit sets identity only for this process, leaving repository config intact.
func Commit(t *testing.T, c *git.Client) {
	t.Helper()
	Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "commit", "--allow-empty", "-m", "test")
}
