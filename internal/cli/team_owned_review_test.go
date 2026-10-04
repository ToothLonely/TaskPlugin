package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestSavedCommandOutputFailureStillPublishes(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	testrepo.Run(t, c, "init", "--bare", "--initial-branch=main", remote)
	testrepo.Run(t, c, "remote", "add", "origin", remote)
	network := func(args ...string) []byte {
		t.Helper()
		r, err := c.RunNetwork(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return r.Stdout
	}
	network("push", "origin", "refs/heads/main:refs/heads/main")
	if err := p.Connect(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	before := network("ls-remote", "origin", "refs/heads/git-task-plan")
	var diagnostic bytes.Buffer
	code := RunWithPlans(ctx, []string{"start", "saved-output-error", "--id", "task-001"}, "test", Streams{
		Out: failedWriter{}, Err: &diagnostic,
	}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 1 || !strings.Contains(diagnostic.String(), "операция выполнена") {
		t.Fatalf("code=%d diagnostic=%q", code, &diagnostic)
	}
	after := network("ls-remote", "origin", "refs/heads/git-task-plan")
	plan, err := p.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item, err := plan.FindID("task-001")
	if err != nil || item.Status != task.Active || len(item.Attempts) != 1 || len(plan.Team.Pending) != 0 || bytes.Equal(before, after) {
		t.Fatalf("saved start was not published after output failure: task=%+v err=%v pending=%d", item, err, len(plan.Team.Pending))
	}
}
