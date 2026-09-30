package hooks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/testrepo"
)

func installFixture(t *testing.T) (Installer, string) {
	t.Helper()
	c := testrepo.New(t)
	testrepo.Run(t, c, "config", "--local", "--unset", "core.hooksPath")
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo.GitDir, "hooks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Installer{Git: c, Binary: binary}, dir
}

func TestInstallNoOpAndRestoreOriginal(t *testing.T) {
	i, dir := installFixture(t)
	ctx := context.Background()
	path := filepath.Join(dir, "post-commit")
	original := []byte("#!/bin/sh\nprintf '%s\\n' \"$0\"\nexit 7\n")
	if err := os.WriteFile(path, original, 0755); err != nil {
		t.Fatal(err)
	}
	before, err := read(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := i.Install(ctx); err != nil || !changed {
		t.Fatalf("install: %v, %v", changed, err)
	}
	installed, err := read(path)
	if err != nil || bytes.Equal(installed.data, original) {
		t.Fatalf("wrapper: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := i.Install(ctx); err != nil || changed {
		t.Fatalf("repeat: %v, %v", changed, err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(info, afterInfo) || !info.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("repeat rewrote wrapper: %v", err)
	}
	if changed, err := i.Uninstall(ctx); err != nil || !changed {
		t.Fatalf("uninstall: %v, %v", changed, err)
	}
	after, err := read(path)
	if err != nil || !equal(before, after) {
		t.Fatalf("foreign hook not restored: %v", err)
	}
	for _, event := range events[1:] {
		if state, err := read(filepath.Join(dir, event)); err != nil || state.exists {
			t.Fatalf("own hook remains: %s %v", event, err)
		}
	}
	if changed, err := i.Uninstall(ctx); err != nil || changed {
		t.Fatalf("repeat uninstall: %v %v", changed, err)
	}
}

func TestUninstallPreservesLaterEdits(t *testing.T) {
	i, dir := installFixture(t)
	ctx := context.Background()
	if _, err := i.Install(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "post-checkout")
	state, err := read(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := append(state.data, []byte("\nprintf 'user change'\n")...)
	if err := os.WriteFile(path, edited, state.mode); err != nil {
		t.Fatal(err)
	}
	first, err := read(filepath.Join(dir, "post-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i.Uninstall(ctx); err == nil {
		t.Fatal("modified wrapper deleted")
	}
	if _, err := i.Install(ctx); err == nil {
		t.Fatal("modified wrapper overwritten")
	}
	after, err := read(path)
	if err != nil || !bytes.Equal(after.data, edited) {
		t.Fatalf("user edit lost: %v", err)
	}
	firstAfter, err := read(filepath.Join(dir, "post-commit"))
	if err != nil || !equal(first, firstAfter) {
		t.Fatalf("preflight mutated earlier hook: %v", err)
	}
}

func TestUnsupportedHookPreflight(t *testing.T) {
	for _, original := range []string{"#!/bin/bash\necho foreign\n", "#!/bin/sh\nlefthook run post-rewrite\n", "#!/bin/sh\r\necho foreign\r\n"} {
		t.Run(original, func(t *testing.T) {
			i, dir := installFixture(t)
			path := filepath.Join(dir, "post-rewrite")
			if err := os.WriteFile(path, []byte(original), 0755); err != nil {
				t.Fatal(err)
			}
			if _, err := i.Install(context.Background()); err == nil {
				t.Fatal("unsupported hook accepted")
			}
			after, err := read(path)
			if err != nil || !bytes.Equal(after.data, []byte(original)) {
				t.Fatalf("foreign hook changed: %v", err)
			}
			for _, event := range events[:3] {
				state, err := read(filepath.Join(dir, event))
				if err != nil || state.exists {
					t.Fatalf("partial preflight install: %s %v", event, err)
				}
			}
		})
	}
}

func TestLocalAndSharedHooksPath(t *testing.T) {
	for _, kind := range []string{"local-git-dir", "relative-git-dir", "external", "command-scope", "global-scope"} {
		t.Run(kind, func(t *testing.T) {
			i, dir := installFixture(t)
			path := filepath.Join(filepath.Dir(dir), "local hooks")
			local := kind == "local-git-dir" || kind == "relative-git-dir"
			if !local {
				path = filepath.Join(t.TempDir(), "shared hooks")
			}
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "command-scope" {
				i.Git.Env = append(i.Git.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0="+path)
			} else if kind == "global-scope" {
				config := filepath.Join(t.TempDir(), "global.gitconfig")
				if err := os.WriteFile(config, nil, 0600); err != nil {
					t.Fatal(err)
				}
				testrepo.Run(t, i.Git, "config", "--file", config, "core.hooksPath", path)
				for n, entry := range i.Git.Env {
					if strings.HasPrefix(entry, "GIT_CONFIG_GLOBAL=") {
						i.Git.Env[n] = "GIT_CONFIG_GLOBAL=" + config
					}
				}
			} else if kind == "relative-git-dir" {
				testrepo.Run(t, i.Git, "config", "--local", "core.hooksPath", ".git/local hooks")
			} else {
				testrepo.Run(t, i.Git, "config", "--local", "core.hooksPath", path)
			}
			_, err := i.Install(context.Background())
			if (err == nil) != local {
				t.Fatalf("%s: %v", kind, err)
			}
			if !local {
				entries, err := os.ReadDir(path)
				if err != nil || len(entries) != 0 {
					t.Fatalf("shared path mutated: %v", err)
				}
			}
		})
	}
}

func TestInterruptedInstallCanResumeOrUninstall(t *testing.T) {
	for _, resume := range []bool{true, false} {
		t.Run(map[bool]string{true: "resume", false: "uninstall"}[resume], func(t *testing.T) {
			i, dir := installFixture(t)
			ctx := context.Background()
			if _, err := i.Install(ctx); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(dir, "post-rewrite")); err != nil {
				t.Fatal(err)
			}
			var err error
			if resume {
				_, err = i.Install(ctx)
			} else {
				_, err = i.Uninstall(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err := read(filepath.Join(dir, "post-rewrite"))
			if err != nil || state.exists != resume {
				t.Fatalf("resume=%v: %v", resume, err)
			}
		})
	}
}

func TestInstallRejectsLockAndSymlink(t *testing.T) {
	i, dir := installFixture(t)
	if err := os.WriteFile(filepath.Join(dir, ".git-task-hooks.lock"), []byte("other writer"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Install(context.Background()); err == nil {
		t.Fatal("lock ignored")
	}
	j, second := installFixture(t)
	target := filepath.Join(t.TempDir(), "foreign")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 9\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(second, "post-commit")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := j.Install(context.Background()); err == nil {
		t.Fatal("symlink accepted")
	}
	if after, err := os.ReadFile(target); err != nil || string(after) != "#!/bin/sh\nexit 9\n" {
		t.Fatalf("symlink target changed: %v", err)
	}
}

func TestCorruptJournalAndPendingFileStopChanges(t *testing.T) {
	for _, kind := range []string{"journal", "pending"} {
		t.Run(kind, func(t *testing.T) {
			i, dir := installFixture(t)
			ctx := context.Background()
			if _, err := i.Install(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := read(filepath.Join(dir, "post-commit"))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ".git-task-post-rewrite.json")
			if kind == "pending" {
				path = filepath.Join(dir, ".git-task-pending-interrupted")
			}
			if err := os.WriteFile(path, []byte("interrupted"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := i.Install(ctx); err == nil {
				t.Fatal("interrupted artifacts ignored by install")
			}
			if _, err := i.Uninstall(ctx); err == nil {
				t.Fatal("interrupted artifacts ignored by uninstall")
			}
			after, err := read(filepath.Join(dir, "post-commit"))
			if err != nil || !equal(before, after) {
				t.Fatalf("earlier hook changed: %v", err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "interrupted" {
				t.Fatalf("artifact lost: %v", err)
			}
		})
	}
}

func TestReplacementPreservesUnexpectedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook")
	before := fileState{exists: false}
	if err := os.WriteFile(path, []byte("foreign edit"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := replace(path, before, fileState{data: []byte("own wrapper"), mode: 0755, exists: true}); err == nil {
		t.Fatal("unexpected file overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign edit" {
		t.Fatalf("foreign edit lost: %q %v", data, err)
	}
}
