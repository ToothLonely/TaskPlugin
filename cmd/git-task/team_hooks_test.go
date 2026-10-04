package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/git"
)

func teamHookPair(t *testing.T, binary string) (hookFixture, hookFixture) {
	t.Helper()
	a := newHookFixture(t, binary)
	remote := filepath.Join(t.TempDir(), "team.git")
	a.runGit("init", "--bare", "--initial-branch=main", remote)
	networkGit(t, a, "push", remote, "refs/heads/main:refs/heads/main")
	a.runGit("remote", "add", "origin", remote)
	a.cli("team", "connect", "--remote", "origin")
	root := filepath.Join(t.TempDir(), "receiver")
	networkGit(t, a, "clone", "--", remote, root)
	c, err := git.New(root)
	if err != nil {
		t.Fatal(err)
	}
	c.Env = append([]string(nil), a.git.Env...)
	b := hookFixture{t: t, ctx: a.ctx, git: c, binary: binary, dir: filepath.Join(root, ".git", "hooks")}
	b.cli("init")
	b.cli("team", "connect", "--remote", "origin")
	b.cli("hooks", "install")
	return a, b
}

func networkGit(t *testing.T, f hookFixture, args ...string) []byte {
	t.Helper()
	r, err := f.git.RunNetwork(f.ctx, args...)
	if err != nil {
		t.Fatalf("network Git %v: %v", args, err)
	}
	return r.Stdout
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestTeamPlanReceivedByRealFetchAndPullHooks(t *testing.T) {
	binary := buildHooksBinary(t)
	for _, mode := range []string{"fetch-plan-only", "pull-ff", "pull-merge", "pull-rebase", "pull-conflict"} {
		t.Run(mode, func(t *testing.T) {
			a, b := teamHookPair(t, binary)
			if mode == "pull-merge" || mode == "pull-rebase" {
				b.write("local.txt", "local\n")
				b.commit("local work")
			}
			if mode == "pull-conflict" {
				b.write("base.txt", "receiver\n")
				b.commit("receiver conflict")
			}
			a.cli("edit", "--id", "task-001", "--title", mode)
			if mode != "fetch-plan-only" {
				a.write("base.txt", "publisher\n")
				a.commit("remote work")
				networkGit(t, a, "push", "origin", "refs/heads/main:refs/heads/main")
			}
			head := string(b.runGit("rev-parse", "HEAD"))
			if mode == "fetch-plan-only" {
				networkGit(t, b, "fetch", "origin")
				if string(b.runGit("rev-parse", "HEAD")) != head {
					t.Fatal("plan fetch changed code HEAD")
				}
			} else {
				option := "--no-rebase"
				if mode == "pull-ff" {
					option = "--ff-only"
				}
				if mode == "pull-rebase" {
					option = "--rebase"
				}
				_, err := b.git.RunNetwork(b.ctx, "pull", option, "--no-edit", "origin")
				if (err != nil) != (mode == "pull-conflict") {
					t.Fatalf("pull outcome: %v", err)
				}
			}
			if got := b.saved(); got.Title != mode {
				t.Fatalf("committed reference hook missed plan: %+v", got)
			}
			before := string(b.planBytes())
			networkGit(t, b, "fetch", "origin")
			if string(b.planBytes()) != before {
				t.Fatal("repeated fetch rewrote plan")
			}
		})
	}
}

func TestMissedReferenceHookRecoveredLocallyWithoutNetwork(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	b.cli("hooks", "uninstall")
	a.cli("edit", "--id", "task-001", "--title", "received later")
	networkGit(t, b, "fetch", "origin")
	if b.saved().Title == "received later" {
		t.Fatal("fetch without hook changed local plan")
	}
	b.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "unreachable.git"))
	b.cli("status", "--json")
	if b.saved().Title != "received later" {
		t.Fatal("status did not recover cached plan")
	}
	before := string(b.planBytes())
	b.cli("sync")
	if string(b.planBytes()) != before {
		t.Fatal("no-op sync changed plan")
	}
}

func TestReferenceConflictKeepsLocalPlanAndForeignHook(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	b.cli("hooks", "uninstall")
	marker := filepath.Join(b.git.Dir, "foreign-hook.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> " + shellQuote(filepath.ToSlash(marker)) + "\ncat >/dev/null\n"
	if err := os.WriteFile(filepath.Join(b.dir, "reference-transaction"), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	b.cli("hooks", "install")
	b.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
	b.cli("edit", "--id", "task-001", "--title", "local title")
	a.cli("edit", "--id", "task-001", "--title", "remote title")
	url := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	b.runGit("remote", "set-url", "origin", url)
	networkGit(t, b, "fetch", "origin")
	if b.saved().Title != "local title" {
		t.Fatal("conflict overwrote local work")
	}
	conflicts, err := filepath.Glob(filepath.Join(b.git.Dir, ".git-task", "conflict-*.json"))
	if err != nil || len(conflicts) == 0 {
		t.Fatalf("remote conflict not saved: %v %v", conflicts, err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(data), "prepared") || !strings.Contains(string(data), "committed") {
		t.Fatalf("foreign handler lost phases: %s %v", data, err)
	}
	out := b.cli("status", "--json")
	if !strings.Contains(string(out), "shared_receive_failed") || !strings.Contains(string(out), "local title") {
		t.Fatalf("local status unavailable after conflict: %s", out)
	}
}
