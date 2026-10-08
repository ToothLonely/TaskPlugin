package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestServerPostReceiveAutomaticallyPublishesCompletion(t *testing.T) {
	binary := buildHooksBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seed := testrepo.New(t)
	testrepo.Commit(t, seed)
	remote := filepath.Join(t.TempDir(), "server.git")
	testrepo.Run(t, seed, "init", "--bare", "--initial-branch=main", remote)
	network := func(c *git.Client, args ...string) []byte {
		t.Helper()
		r, err := c.RunNetwork(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return r.Stdout
	}
	network(seed, "push", remote, "refs/heads/main:refs/heads/main")
	clone := func(name string) *git.Client {
		root := filepath.Join(t.TempDir(), name)
		network(seed, "clone", "--", remote, root)
		c, err := git.New(root)
		if err != nil {
			t.Fatal(err)
		}
		c.Env = append([]string(nil), seed.Env...)
		return c
	}
	a, b, worker := clone("developer-a"), clone("developer-b"), clone("server-worker")
	cli := func(c *git.Client, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Env = c.Dir, c.Env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("CLI %v: %v %s", args, err, out)
		}
	}
	readPlan := func(c *git.Client) task.Plan {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.json"))
		if err != nil {
			t.Fatal(err)
		}
		var p task.Plan
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cli(a, "init", "--target", "main")
	cli(a, "add", "Server integration")
	id := readPlan(a).Tasks[0].ID
	cli(a, "team", "connect", "--remote", "origin")
	for _, c := range []*git.Client{b, worker} {
		cli(c, "init", "--target", "main")
		cli(c, "team", "connect", "--remote", "origin")
	}
	cli(a, "start", "alice", "--id", id)
	cli(b, "start", "empty-bob", "--id", id)
	network(b, "push", "origin", "refs/heads/empty-bob:refs/heads/empty-bob")
	testrepo.Commit(t, a)
	network(a, "push", "origin", "refs/heads/alice:refs/heads/alice")
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "git-task-post-receive.sample"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "hooks", "post-receive"), script, 0755); err != nil {
		t.Fatal(err)
	}
	a.Env = append(a.Env, "GIT_TASK_WORKER="+worker.Dir, "GIT_TASK_BINARY="+binary, "GIT_TASK_TARGET=main")
	testrepo.Run(t, a, "checkout", "main")
	testrepo.Run(t, a, "merge", "--ff-only", "alice")
	network(a, "push", "origin", "refs/heads/main:refs/heads/main")
	cli(b, "team", "fetch")
	item, err := readPlan(b).FindID(id)
	if err != nil || len(item.Attempts) != 2 || item.Status != task.Active {
		t.Fatalf("server lost approaches: %+v %v", item, err)
	}
	for _, attempt := range item.Attempts {
		if attempt.Branch == "alice" && (attempt.Status != task.Done || attempt.Completion == nil || attempt.Completion.Source != task.Merge) {
			t.Fatalf("server did not finish integrated work: %+v", attempt)
		}
		if attempt.Branch == "empty-bob" && (attempt.Status != task.Active || attempt.Completion != nil) {
			t.Fatalf("server fabricated empty completion: %+v", attempt)
		}
	}
	network(a, "fetch", "origin", "refs/heads/git-task-plan")
	data := testrepo.Run(t, a, "show", "FETCH_HEAD:plan.json")
	if strings.Contains(string(data), "server_events") || strings.Contains(string(data), "observation") || strings.Contains(string(data), "pending") {
		t.Fatal("local worker data published")
	}
	if _, err := task.ValidateShared(data); err != nil {
		t.Fatal(err)
	}
	b.Env = append(b.Env, "GIT_TASK_WORKER="+worker.Dir, "GIT_TASK_BINARY="+binary, "GIT_TASK_TARGET=main")
	if err := os.WriteFile(filepath.Join(b.Dir, "bob.txt"), []byte("Bob work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, b, "add", "--", "bob.txt")
	testrepo.Commit(t, b)
	network(b, "push", "origin", "refs/heads/empty-bob:refs/heads/empty-bob")
	testrepo.Run(t, b, "checkout", "main")
	network(b, "fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
	testrepo.Run(t, b, "merge", "--ff-only", "origin/main")
	serverBefore := strings.TrimSpace(string(testrepo.Run(t, b, "rev-parse", "main")))
	testrepo.Run(t, b, "merge", "--no-edit", "empty-bob")
	serverAfter := strings.TrimSpace(string(testrepo.Run(t, b, "rev-parse", "main")))
	network(b, "push", "origin", "refs/heads/main:refs/heads/main")
	cli(a, "team", "fetch")
	item, err = readPlan(a).FindID(id)
	if err != nil || item.Status != task.Done || len(item.Attempts) != 2 {
		t.Fatalf("server did not close all approaches: %+v %v", item, err)
	}
	for _, attempt := range item.Attempts {
		if attempt.Status != task.Done || attempt.Completion == nil || attempt.Completion.Source != task.Merge {
			t.Fatalf("server completion missing: %+v", attempt)
		}
	}
	metadataBefore := network(a, "ls-remote", "origin", "refs/heads/git-task-plan")
	cli(worker, "team", "reconcile", "--before", serverBefore, "--after", serverAfter)
	if !bytes.Equal(metadataBefore, network(a, "ls-remote", "origin", "refs/heads/git-task-plan")) {
		t.Fatal("repeated server event created a metadata commit")
	}
	cli(b, "team", "fetch")
	cli(b, "start", "server-squash", "--id", id, "--again")
	if err := os.WriteFile(filepath.Join(b.Dir, "squash.txt"), []byte("Squashed work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, b, "add", "--", "squash.txt")
	originalEnv := b.Env
	b.Env = append(append([]string(nil), b.Env...), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	testrepo.Run(t, b, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "commit", "-m", "Source work before server squash")
	sourceTip := strings.TrimSpace(string(testrepo.Run(t, b, "rev-parse", "server-squash")))
	network(b, "push", "origin", "refs/heads/server-squash:refs/heads/server-squash")
	testrepo.Run(t, b, "checkout", "main")
	testrepo.Run(t, b, "merge", "--squash", "server-squash")
	testrepo.Run(t, b, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgSign=false", "commit", "-m", "Squashed integration on server target")
	b.Env = originalEnv
	targetTip := strings.TrimSpace(string(testrepo.Run(t, b, "rev-parse", "main")))
	t.Logf("squash fixture tips: source=%s target=%s", sourceTip, targetTip)
	if sourceTip == targetTip {
		t.Fatal("invalid squash fixture: source and target commit IDs are equal")
	}
	included, err := b.IsAncestor(ctx, sourceTip, targetTip)
	if err != nil {
		t.Fatalf("check squash fixture ancestry: %v", err)
	}
	if included {
		t.Fatal("invalid squash fixture: source commit is reachable from target")
	}
	t.Log("squash fixture verified: source is not an ancestor of target")
	network(b, "push", "origin", "refs/heads/main:refs/heads/main")
	cli(a, "team", "fetch")
	item, err = readPlan(a).FindID(id)
	if err != nil || item.Status != task.Active || len(item.Attempts) != 3 {
		t.Logf("server graph:\n%s", testrepo.Run(t, seed, "--git-dir="+remote, "log", "--all", "--format=%H %P %s"))
		for _, attempt := range item.Attempts {
			if attempt.Completion != nil {
				t.Logf("approach %s (%s) completion: %+v", attempt.ID, attempt.Branch, *attempt.Completion)
			}
		}
		t.Fatalf("server falsely completed squash: %+v %v", item, err)
	}
	for _, attempt := range item.Attempts {
		if attempt.Branch == "server-squash" && (attempt.Status != task.Active || attempt.Completion != nil) {
			t.Fatalf("server fabricated squash completion: %+v", attempt)
		}
	}
}
