package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func testBinaryLifecycle(t *testing.T, ctx context.Context, gitPath, execPath string) {
	t.Helper()
	c := testrepo.New(t)
	testrepo.Commit(t, c)
	run := func(want int, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, gitPath, append([]string{"--exec-path=" + execPath, "task"}, args...)...)
		cmd.Dir, cmd.Env = c.Dir, c.Env
		var out, diagnostic bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != want {
			t.Fatalf("git task %v: code=%d want=%d %s", args, code, want, &diagnostic)
		}
	}
	read := func() task.Plan {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.json"))
		if err != nil {
			t.Fatal(err)
		}
		var plan task.Plan
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	item := func() task.Task {
		t.Helper()
		got, err := read().FindID("task-001")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	run(0, "init")
	run(0, "add", "Первая")
	run(0, "add", "Вторая")
	testrepo.FixturePlanFile(t, c)
	run(0, "edit", "--id=task-001", "--title=Точное имя 🙂", "--description=Описание")
	run(0, "move", "--id=task-001", "--end")
	if plan := read(); !reflect.DeepEqual(plan.Order, []string{"task-002", "task-001"}) || item().Number != "T-001" {
		t.Fatalf("move changed identity/order: %+v", plan)
	}
	run(0, "start", "first", "--title=Точное имя 🙂")
	started := item()
	run(0, "pause", "--id=task-001")
	run(0, "pause", "--id=task-001")
	testrepo.Run(t, c, "checkout", "main")
	run(0, "resume", "--id=task-001")
	run(1, "resume", "--id=task-001")
	if got := item(); got.Status != task.Active || !reflect.DeepEqual(got.ActiveAttempt, started.ActiveAttempt) {
		t.Fatalf("resume lost attempt: %+v", got)
	}
	run(0, "complete", "--id=task-001", "--yes")
	manual := item().Attempts[0]
	run(0, "complete", "--id=task-001", "--yes")
	run(1, "start", "refused", "--id=task-001")
	run(0, "start", "second", "--title=Точное имя 🙂", "--again")
	run(0, "sync")
	if got := item(); got.Status != task.Active || len(got.Attempts) != 2 || !reflect.DeepEqual(got.Attempts[0], manual) {
		t.Fatalf("old manual proof closed new attempt: %+v", got)
	}
	testrepo.Commit(t, c)
	run(0, "sync")
	testrepo.Run(t, c, "checkout", "main")
	testrepo.Run(t, c, "merge", "--ff-only", "second")
	run(0, "sync")
	done := item()
	if done.Status != task.Done || len(done.Attempts) != 2 || !reflect.DeepEqual(done.Attempts[0], manual) || done.ActiveAttempt != nil {
		t.Fatalf("complete/again/merge lost history: %+v", done)
	}
	run(0, "sync")
	if len(item().Attempts) != 2 {
		t.Fatal("repeated sync duplicated completion")
	}
	run(0, "start", "third", "--id=task-001", "--again")
	run(0, "pause", "--id=task-001")
	testrepo.Run(t, c, "checkout", "main")
	testrepo.Run(t, c, "branch", "-m", "third", "renamed")
	run(1, "resume", "--id=task-001")
	run(0, "attach", "renamed", "--id=task-001", "--rebind")
	run(0, "attach", "renamed", "--id=task-001", "--rebind")
	run(0, "resume", "--id=task-001")
	before := item()
	run(0, "archive", "--id=task-001")
	run(0, "archive", "--id=task-001")
	if got := item(); got.Status != task.Archived || !reflect.DeepEqual(got.ActiveAttempt, before.ActiveAttempt) || !reflect.DeepEqual(got.Attempts, before.Attempts) {
		t.Fatalf("archive erased history: %+v", got)
	}
	run(0, "attach", "renamed", "--id=task-002")
	run(1, "start", "bad-archive", "--id=task-001", "--again")
	if commit, err := c.BranchCommit(ctx, "renamed"); err != nil || commit == "" {
		t.Fatalf("archive removed branch: %q %v", commit, err)
	}
}
