package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/task"
)

func TestExchangeCLIWithHooksAndForeignHandlers(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	a.runGit("config", "branch.main.remote", "origin")
	a.runGit("config", "branch.main.merge", "refs/heads/main")
	marker := filepath.Join(a.git.Dir, ".git", "foreign-merge")
	if err := os.WriteFile(filepath.Join(a.dir, "post-merge"), []byte("#!/bin/sh\nprintf done > "+shellQuote(filepath.ToSlash(marker))+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	a.cli("hooks", "install")
	a.cli("start", "work")
	a.write("work.txt", "work\n")
	a.commit("work")
	a.runGit("checkout", "main")
	a.runGit("merge", "--no-ff", "--no-edit", "work")
	if a.saved().Status != task.Done {
		t.Fatal("merge hook did not complete task")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("foreign handler not called", err)
	}
	if out := string(a.cli("push")); !strings.Contains(out, "Код отправлен") {
		t.Fatal(out)
	}
	b.runGit("config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	if out := string(b.cli("pull")); !strings.Contains(out, "Код обновлён") {
		t.Fatal(out)
	}
	if b.saved().Status != task.Done {
		t.Fatal("completion not received")
	}
	if got := string(b.runGit("show", "HEAD:work.txt")); got != "work\n" {
		t.Fatal(got)
	}
	a.cli("push")
	b.cli("pull")
}

func TestExchangeCLIPartialPublicationFailure(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	a.runGit("config", "branch.main.remote", "origin")
	a.runGit("config", "branch.main.merge", "refs/heads/main")
	a.write("new.txt", "code\n")
	a.commit("new code")
	remote := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	hook := filepath.Join(remote, "hooks", "update")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nif test \"$1\" = refs/heads/git-task-plan; then exit 1; fi\n"), 0755); err != nil {
		t.Fatal(err)
	}
	a.cli("add", "queued")
	out, code := a.command("push")
	if code != 1 || !strings.Contains(string(out), "код отправлен") || !strings.Contains(string(out), "team publish") {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	a.cli("team", "publish")
	b.cli("pull")
	if got := string(b.runGit("show", "HEAD:new.txt")); got != "code\n" {
		t.Fatal(got)
	}
}

func TestExchangeCLIPullHookFailureReportsChangedHead(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	a.runGit("config", "branch.main.remote", "origin")
	a.runGit("config", "branch.main.merge", "refs/heads/main")
	a.write("new.txt", "new\n")
	a.commit("new")
	a.cli("push")
	b.cli("hooks", "uninstall")
	if err := os.WriteFile(filepath.Join(b.dir, "post-merge"), []byte("#!/bin/sh\nexit 17\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out, code := b.command("pull")
	if got := string(b.runGit("show", "HEAD:new.txt")); got != "new\n" {
		t.Fatal(got)
	}
	if code != 0 && !strings.Contains(string(out), "HEAD уже изменился") {
		t.Fatalf("code=%d out=%s", code, out)
	}
}
