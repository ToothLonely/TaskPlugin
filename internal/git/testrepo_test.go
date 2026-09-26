package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolatedClient removes inherited repository, identity, config, and transport
// overrides. All Git configuration and hooks belong to this temporary tree.
func isolatedClient(t *testing.T) *Client {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "репозиторий с пробелами & (test)")
	for _, path := range []string{dir, filepath.Join(base, "home"), filepath.Join(base, "hooks"), filepath.Join(base, "template")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(base, "config")
	if err := os.WriteFile(config, []byte("[user]\n name = Git Task Test\n email = test@example.invalid\n[commit]\n gpgSign = false\n[core]\n autocrlf = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Env = isolatedEnvironment(base, config)
	version := mustRun(t, c, "--version")
	t.Logf("%s", outputLine(version.Stdout))
	mustRun(t, c, "init", "--initial-branch=main", "--template="+filepath.Join(base, "template"))
	mustRun(t, c, "config", "--local", "core.hooksPath", filepath.Join(base, "hooks"))
	return c
}

func isolatedEnvironment(base, config string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "GIT_") || key == "HOME" || key == "USERPROFILE" || key == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, entry)
	}
	home := filepath.Join(base, "home")
	return append(env, "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+config, "GIT_CONFIG_GLOBAL="+config,
		"GIT_ATTR_NOSYSTEM=1", "GIT_CEILING_DIRECTORIES="+filepath.Dir(base))
}

func mustRun(t *testing.T, c *Client, args ...string) Result {
	t.Helper()
	result, err := c.Run(context.Background(), args...)
	if err != nil {
		t.Fatalf("Git %q: %v", args, err)
	}
	return result
}
