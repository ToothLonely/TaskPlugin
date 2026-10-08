package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestInitTemplateAndManualTasksStart(t *testing.T) {
	ctx := context.Background()
	c := testrepo.New(t)
	testrepo.Commit(t, c)
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plans{git: c, store: storage.New(c.Dir, repo.ExcludePath, c)}
	if result, _, err := p.InitTemplate(ctx, false); err != nil || !result.Created || !result.Draft {
		t.Fatalf("Init: result=%+v err=%v", result, err)
	}
	path := filepath.Join(c.Dir, ".git-task", "plan.json")
	id := "9f86d081884c4d659a2feaa0c55ad015"
	filled := []byte(`{"target_branch":"main","tasks":[{"id":"9f86d081884c4d659a2feaa0c55ad015","title":"Search"}]}`)
	if err := os.WriteFile(path, filled, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Start(ctx, StartOptions{Branch: "feature/rejected"}); !errors.Is(err, storage.ErrTemplate) {
		t.Fatalf("start before apply: %v", err)
	}
	if result, _, err := p.InitTemplate(ctx, true); err != nil || !result.Applied {
		t.Fatalf("apply: result=%+v err=%v", result, err)
	}
	base, err := c.BranchCommit(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	started, err := p.Start(ctx, StartOptions{Branch: "feature/search"})
	if err != nil {
		t.Fatal(err)
	}
	if started.ID != id || started.Status != task.Active || len(started.Attempts) != 1 {
		t.Fatalf("start did not record the manual task: %+v", started)
	}
	a := started.Attempts[0]
	if a.ID == "" || a.Author == "" || a.Status != task.Active || a.Branch != "feature/search" || a.OriginalBranch != a.Branch || a.TargetBranch != "main" || a.BaseCommit != base || a.StartedAt == nil || a.Observation == nil || a.Completion != nil {
		t.Fatalf("incomplete start attempt: %+v", a)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(`"attempts"`)) {
		t.Fatalf("attempt not persisted: %s err=%v", data, err)
	}
	for _, apply := range []bool{false, true} {
		if result, _, err := p.InitTemplate(ctx, apply); err != nil || result.Draft || result.Applied || result.Created {
			t.Fatalf("repeat after start: %+v %v", result, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, after) {
			t.Fatalf("repeat init changed history: %s %v", after, err)
		}
	}
}

func TestAddAndStartNewGeneratedIDsPreserveManualID(t *testing.T) {
	ctx := context.Background()
	p, c := startFixture(t)
	snapshot, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := snapshot.Plan
	next.Tasks[0].ID = "001"
	next.Order[0] = "001"
	next.Revision++
	if _, err := p.store.Save(ctx, snapshot, next); err != nil {
		t.Fatal(err)
	}
	item, err := p.Add(ctx, "Added task", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := p.Start(ctx, StartOptions{Branch: "feature/new", New: "Started task"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{item.ID, started.ID} {
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 || id == "001" {
			t.Fatalf("invalid generated ID %q: %v", id, err)
		}
	}
	if item.ID == started.ID {
		t.Fatal("two tasks share an ID")
	}
	var saved task.Plan
	if err := json.Unmarshal(planBytes(t, c), &saved); err != nil {
		t.Fatal(err)
	}
	if _, err := saved.FindID("001"); err != nil {
		t.Fatalf("manual ID was replaced: %v", err)
	}
}
