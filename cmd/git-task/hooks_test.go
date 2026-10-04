package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

type hookFixture struct {
	t      *testing.T
	ctx    context.Context
	git    *git.Client
	binary string
	dir    string
}

func buildHooksBinary(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	dir := filepath.Join(t.TempDir(), "tools 'quoted' с пробелами")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "git-task"+suffix)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", binary, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return binary
}

func newHookFixture(t *testing.T, binary string) hookFixture {
	t.Helper()
	c := testrepo.New(t)
	testrepo.Run(t, c, "config", "--local", "--unset", "core.hooksPath")
	testrepo.Run(t, c, "config", "--local", "user.name", "Test")
	testrepo.Run(t, c, "config", "--local", "user.email", "test@example.invalid")
	testrepo.Run(t, c, "config", "--local", "commit.gpgSign", "false")
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo.GitDir, "hooks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tmpDir := filepath.Join(t.TempDir(), "hook-input")
	var env []string
	for _, entry := range c.Env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "TMPDIR") {
			env = append(env, entry)
		}
	}
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatal(err)
	}
	c.Env = append(env, "TMPDIR="+filepath.ToSlash(tmpDir))
	copyDir := filepath.Join(t.TempDir(), "binary 'copy' с пробелами")
	if err := os.MkdirAll(copyDir, 0700); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(copyDir, filepath.Base(binary))
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, data, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	f := hookFixture{t: t, ctx: ctx, git: c, binary: copyPath, dir: dir}
	f.write("base.txt", "base\n")
	f.commit("base")
	f.cli("init")
	f.cli("add", "Задача hooks")
	testrepo.FixturePlanFile(t, c)
	return f
}

func (f hookFixture) command(args ...string) ([]byte, int) {
	f.t.Helper()
	cmd := exec.CommandContext(f.ctx, f.binary, args...)
	cmd.Dir, cmd.Env = f.git.Dir, f.git.Env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		f.t.Fatal(err)
	}
	return out, exit.ExitCode()
}

func (f hookFixture) cli(args ...string) []byte {
	f.t.Helper()
	out, code := f.command(args...)
	if code != 0 {
		f.t.Fatalf("CLI %q code=%d: %s", args, code, out)
	}
	return out
}

func (f hookFixture) runGit(args ...string) []byte {
	f.t.Helper()
	r, err := f.git.Run(f.ctx, args...)
	if err != nil {
		f.t.Fatalf("Git %q: %v", args, err)
	}
	return r.Stdout
}

func (f hookFixture) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.git.Dir, name), []byte(content), 0600); err != nil {
		f.t.Fatal(err)
	}
	f.runGit("add", "--", name)
}

func (f hookFixture) commit(message string) {
	f.t.Helper()
	f.runGit("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "commit", "--allow-empty", "-m", message)
}

func (f hookFixture) saved() task.Task {
	f.t.Helper()
	var plan task.Plan
	if err := json.Unmarshal(f.planBytes(), &plan); err != nil {
		f.t.Fatal(err)
	}
	t, err := plan.FindID("task-001")
	if err != nil {
		f.t.Fatal(err)
	}
	return t
}

func (f hookFixture) planBytes() []byte {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.git.Dir, ".git-task", "plan.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}

func (f hookFixture) hook(event, body string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, event), []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		f.t.Fatal(err)
	}
}

func TestRealHooks(t *testing.T) {
	binary := buildHooksBinary(t)
	for _, ff := range []bool{false, true} {
		name := "merge"
		if ff {
			name = "fast-forward"
		}
		t.Run(name, func(t *testing.T) {
			f := newHookFixture(t, binary)
			f.hook("post-merge", "marker=$(git rev-parse --git-path hooks/merge-marker-seen)\nif [ -f \"$(git rev-parse --git-path MERGE_HEAD)\" ]; then\n    printf '%s\\n' present > \"$marker\"\nelse\n    printf '%s\\n' absent > \"$marker\"\nfi")
			f.cli("hooks", "install")
			out := f.cli("start", "feature")
			if strings.Contains(string(out), "Предупреждение") {
				t.Fatalf("start recursively tracked pending operation: %s", out)
			}
			f.write("feature.txt", "own work\n")
			f.commit("work")
			active := f.saved()
			if active.Status != task.Active || active.ActiveAttempt.Observation.WorkCommit == "" {
				t.Fatalf("commit not observed: %+v", active)
			}
			f.runGit("checkout", "main")
			mode := "--no-ff"
			if ff {
				mode = "--ff-only"
			}
			f.runGit("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", mode, "-m", "integrate", "feature")
			marker, err := os.ReadFile(filepath.Join(f.dir, "merge-marker-seen"))
			wantMarker := "present\n"
			if ff {
				wantMarker = "absent\n"
			}
			if err != nil || string(marker) != wantMarker {
				t.Fatalf("post-merge state: %q %v", marker, err)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(f.dir), "MERGE_HEAD")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Git did not clean up merge state: %v", err)
			}
			done := f.saved()
			if done.Status != task.Done || done.ActiveAttempt != nil || len(done.Attempts) != 1 || done.Attempts[0].ID != active.ActiveAttempt.ID || len(done.Warnings) != 0 {
				t.Fatalf("merge not completed: %+v", done)
			}
			before := f.planBytes()
			f.cli("_hook", "post-merge", "0")
			f.cli("sync")
			if !bytes.Equal(before, f.planBytes()) {
				t.Fatal("duplicate hook/sync changed completed attempt")
			}
			f.cli("start", "feature-again", "--id", "task-001", "--again")
			f.commit("second work")
			f.runGit("checkout", "main")
			f.runGit("merge", "--ff-only", "feature-again")
			if done := f.saved(); done.Status != task.Done || len(done.Attempts) != 2 || done.Attempts[0].ID == done.Attempts[1].ID {
				t.Fatalf("again history: %+v", done)
			}
			before = f.planBytes()
			f.cli("hooks", "uninstall")
			if !bytes.Equal(before, f.planBytes()) {
				t.Fatal("uninstall changed plan")
			}
			f.cli("sync")
		})
	}
	t.Run("unfinished-merge-rejects-hook", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.cli("hooks", "install")
		f.cli("start", "feature")
		f.write("feature.txt", "own work\n")
		f.commit("work")
		active := f.saved()
		f.runGit("checkout", "main")
		f.runGit("merge", "--no-ff", "--no-commit", "feature")
		mergePath := filepath.Join(filepath.Dir(f.dir), "MERGE_HEAD")
		before, err := os.ReadFile(mergePath)
		if err != nil {
			t.Fatal(err)
		}
		out := f.cli("_hook", "post-merge", "0")
		item := f.saved()
		if item.Status != task.Active || item.ActiveAttempt == nil || item.ActiveAttempt.ID != active.ActiveAttempt.ID || len(item.Attempts) != 1 || len(item.Warnings) != 1 || item.Warnings[0].Code != "git_in_progress" || !strings.Contains(string(out), "Предупреждение") {
			t.Fatalf("unfinished merge accepted: %+v\n%s", item, out)
		}
		after, err := os.ReadFile(mergePath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("Git state changed: %q %v", after, err)
		}
		planBefore := f.planBytes()
		f.cli("_hook", "post-merge", "0")
		if !bytes.Equal(planBefore, f.planBytes()) {
			t.Fatal("repeat unfinished hook changed plan")
		}
	})
	t.Run("conflict-resolution", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.cli("hooks", "install")
		f.cli("start", "feature")
		f.write("base.txt", "feature\n")
		f.commit("feature")
		f.runGit("checkout", "main")
		f.write("base.txt", "target\n")
		f.commit("target")
		_, err := f.git.Run(f.ctx, "merge", "feature")
		var command *git.CommandError
		if !errors.As(err, &command) || command.Result.ExitCode != 1 {
			t.Fatalf("expected conflict: %v", err)
		}
		if f.saved().Status != task.Active {
			t.Fatal("conflict prematurely completed task")
		}
		f.cli("_hook", "post-merge", "0")
		if item := f.saved(); item.Status != task.Active || len(item.Attempts) != 1 || len(item.Warnings) != 1 || item.Warnings[0].Code != "git_in_progress" {
			t.Fatalf("hook bypassed conflict: %+v", item)
		}
		f.write("base.txt", "resolved\n")
		f.commit("resolve")
		if done := f.saved(); done.Status != task.Done || len(done.Attempts) != 1 {
			t.Fatalf("resolution commit not tracked: %+v", done)
		}
	})
	t.Run("squash-does-not-complete", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.cli("hooks", "install")
		f.cli("start", "feature")
		f.write("feature.txt", "feature\n")
		f.commit("feature")
		f.runGit("checkout", "main")
		f.runGit("merge", "--squash", "feature")
		f.commit("squashed")
		if got := f.saved(); got.Status != task.Active || len(got.Attempts) != 1 {
			t.Fatalf("false squash completion: %+v", got)
		}
	})
	t.Run("sync-without-hooks", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.cli("start", "feature")
		f.write("feature.txt", "feature\n")
		f.commit("feature")
		f.cli("sync")
		f.runGit("checkout", "main")
		f.runGit("merge", "--ff-only", "feature")
		if f.saved().Status != task.Active {
			t.Fatal("uninstalled hook still ran")
		}
		f.cli("sync")
		if f.saved().Status != task.Done {
			t.Fatal("sync without hooks failed")
		}
	})
	t.Run("chain-arguments-stdin-and-exit", func(t *testing.T) {
		baseline := newHookFixture(t, binary)
		baseline.hook("post-checkout", "exit 9")
		_, baselineErr := baseline.git.Run(baseline.ctx, "checkout", "main")
		var baselineExit *git.CommandError
		if !errors.As(baselineErr, &baselineExit) || baselineExit.Result.ExitCode == 0 {
			t.Fatalf("foreign checkout baseline: %v", baselineErr)
		}
		f := newHookFixture(t, binary)
		f.hook("post-commit", `printf 'commit\n' >> foreign-events
exit 7`)
		f.hook("post-checkout", `printf '%s\n' "$0" "$@" > foreign-checkout
printf 'checkout\n' >> foreign-events
exit 9`)
		f.hook("post-rewrite", `printf '%s\n' "$@" > foreign-rewrite-args
cat > foreign-rewrite-input
exit 11`)
		f.cli("hooks", "install")
		f.cli("hooks", "install")
		if out, code := f.command("start", "feature"); code != 1 || !strings.Contains(string(out), "задача уже запущена") {
			t.Fatalf("foreign checkout result during start: code=%d %s", code, out)
		}
		if f.saved().Status != task.Active {
			t.Fatal("completed checkout effect lost task binding")
		}
		f.write("work.txt", "work\n")
		f.commit("work")
		if got := f.saved(); got.ActiveAttempt.Observation.WorkCommit == "" {
			t.Fatal("foreign nonzero commit suppressed tracking")
		}
		if data, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-events")); err != nil || string(data) != "checkout\ncommit\n" {
			t.Fatalf("duplicate chain: %q %v", data, err)
		}
		old := strings.TrimSpace(string(f.runGit("rev-parse", "HEAD")))
		f.runGit("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "commit", "--amend", "-m", "amended")
		newOID := strings.TrimSpace(string(f.runGit("rev-parse", "HEAD")))
		if data, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-rewrite-input")); err != nil || string(data) != old+" "+newOID+"\n" {
			t.Fatalf("rewrite stdin: %q %v", data, err)
		}
		if data, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-rewrite-args")); err != nil || string(data) != "amend\n" {
			t.Fatalf("rewrite argv: %q %v", data, err)
		}
		_, err := f.git.Run(f.ctx, "checkout", "main")
		var command *git.CommandError
		if !errors.As(err, &command) || command.Result.ExitCode != baselineExit.Result.ExitCode {
			t.Fatalf("foreign checkout exit lost: %v", err)
		}
		if head := strings.TrimSpace(string(f.runGit("symbolic-ref", "HEAD"))); head != "refs/heads/main" {
			t.Fatalf("checkout did not happen: %s", head)
		}
		args, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-checkout"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(string(args), "\n"), "\n")
		if len(lines) != 4 || !strings.HasSuffix(filepath.ToSlash(lines[0]), "/post-checkout") || lines[1] != newOID || lines[3] != "1" {
			t.Fatalf("checkout argv/$0: %q", args)
		}
		_, err = f.git.Run(f.ctx, "hook", "run", "post-checkout", "--", newOID, lines[2], "1")
		if !errors.As(err, &command) || command.Result.ExitCode != 9 {
			t.Fatalf("foreign hook exact exit lost: %v", err)
		}
	})
	t.Run("tracking-write-lock-warns", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.cli("hooks", "install")
		f.cli("start", "feature")
		before := f.planBytes()
		lockPath := filepath.Join(f.git.Dir, ".git-task", "write.lock")
		if err := os.WriteFile(lockPath, []byte("other writer"), 0600); err != nil {
			t.Fatal(err)
		}
		r, err := f.git.Run(f.ctx, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "work")
		if err != nil || !strings.Contains(string(r.Stderr), "Git-операция уже произошла") {
			t.Fatalf("commit blocked or no warning: %v %s", err, r.Stderr)
		}
		if !bytes.Equal(before, f.planBytes()) {
			t.Fatal("locked plan changed")
		}
		if err := os.Remove(lockPath); err != nil {
			t.Fatal(err)
		}
		f.cli("sync")
		if f.saved().ActiveAttempt.Observation.WorkCommit == "" {
			t.Fatal("sync did not recover missed commit")
		}
	})
	t.Run("rebase-stream", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.hook("post-rewrite", `printf '%s\n' "$@" > foreign-rewrite-args
cat > foreign-rewrite-input`)
		f.cli("hooks", "install")
		f.cli("start", "feature")
		f.write("first.txt", "first\n")
		f.commit("first")
		f.write("second.txt", "second\n")
		f.commit("second")
		old := strings.Fields(string(f.runGit("rev-list", "--reverse", "main..feature")))
		f.runGit("checkout", "main")
		f.write("target.txt", "target\n")
		f.commit("target")
		f.runGit("checkout", "feature")
		f.runGit("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "rebase", "main")
		newOIDs := strings.Fields(string(f.runGit("rev-list", "--reverse", "main..feature")))
		if len(old) != 2 || len(newOIDs) != 2 {
			t.Fatalf("rebase commits: %q %q", old, newOIDs)
		}
		want := old[0] + " " + newOIDs[0] + "\n" + old[1] + " " + newOIDs[1] + "\n"
		if data, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-rewrite-input")); err != nil || string(data) != want {
			t.Fatalf("rebase stdin: %q want=%q err=%v", data, want, err)
		}
		if data, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-rewrite-args")); err != nil || string(data) != "rebase\n" {
			t.Fatalf("rebase argv: %q %v", data, err)
		}
		if got := f.saved(); got.Status != task.Active || len(got.Attempts) != 1 {
			t.Fatalf("rebase falsely completed: %+v", got)
		}
	})
	t.Run("missing-binary-preserves-foreign", func(t *testing.T) {
		f := newHookFixture(t, binary)
		f.hook("post-commit", `printf 'foreign\n' >> foreign-events`)
		f.cli("hooks", "install")
		if err := os.Remove(f.binary); err != nil {
			t.Fatal(err)
		}
		r, err := f.git.Run(f.ctx, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "work")
		if err != nil || !strings.Contains(string(r.Stderr), "git-task:") {
			t.Fatalf("missing binary blocked Git: %v %s", err, r.Stderr)
		}
		if data, err := os.ReadFile(filepath.Join(f.git.Dir, "foreign-events")); err != nil || string(data) != "foreign\n" {
			t.Fatalf("foreign hook lost: %q %v", data, err)
		}
	})
}
