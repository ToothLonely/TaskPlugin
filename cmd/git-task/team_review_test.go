package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/task"
)

func TestRejectedTeamStartPreservesQueueAndRemote(t *testing.T) {
	a, _ := teamHookPair(t, buildHooksBinary(t))
	id := a.saved().ID
	url := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	a.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
	a.cli("add", "Pending earlier action")
	a.runGit("remote", "set-url", "origin", url)
	before := a.planBytes()
	remoteBefore := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	cmd := exec.CommandContext(a.ctx, a.binary, "start", "main", "--id", id)
	cmd.Dir, cmd.Env = a.git.Dir, a.git.Env
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("occupied branch accepted: %s", out)
	}
	remoteAfter := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	if !bytes.Equal(before, a.planBytes()) || !bytes.Equal(remoteBefore, remoteAfter) {
		t.Fatal("rejected start changed the local plan or published earlier queued work")
	}
}

func TestTeamRejectedRequestsPreserveExistingQueue(t *testing.T) {
	a, _ := teamHookPair(t, buildHooksBinary(t))
	id := a.saved().ID
	url := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	a.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
	a.cli("add", "Existing queued action")
	a.runGit("remote", "set-url", "origin", url)
	before := a.planBytes()
	remoteBefore := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	for _, args := range [][]string{
		{"start", "free", "--id", "missing-id"},
		{"start", "--id", id},
		{"start", "free", "--id", id, "--again"},
		{"complete", "--id", id},
		{"attach", "missing-branch", "--id", id},
	} {
		cmd := exec.CommandContext(a.ctx, a.binary, args...)
		cmd.Dir, cmd.Env = a.git.Dir, a.git.Env
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("rejected request accepted %v: %s", args, out)
		}
		remoteAfter := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
		if !bytes.Equal(before, a.planBytes()) || !bytes.Equal(remoteBefore, remoteAfter) {
			t.Fatalf("rejected request published queue: %v", args)
		}
	}
}

func TestTeamStartHookFailurePublishesConfirmedMutation(t *testing.T) {
	a, _ := teamHookPair(t, buildHooksBinary(t))
	if err := os.WriteFile(filepath.Join(a.dir, "post-checkout"), []byte("#!/bin/sh\nexit 7\n"), 0755); err != nil {
		t.Fatal(err)
	}
	before := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	cmd := exec.CommandContext(a.ctx, a.binary, "start", "confirmed-hook-error", "--id", a.saved().ID)
	cmd.Dir, cmd.Env = a.git.Dir, a.git.Env
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "задача уже запущена") {
		t.Fatalf("expected confirmed Git/hook error: %s %v", out, err)
	}
	if item := a.saved(); item.Status != task.Active || len(item.Attempts) != 1 {
		t.Fatalf("confirmed mutation lost: %+v", item)
	}
	after := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	if bytes.Equal(before, after) {
		t.Fatal("confirmed mutation not published after hook error")
	}
}
