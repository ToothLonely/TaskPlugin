package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestStatusIndexedReportRetainsOrderAndOwnership(t *testing.T) {
	_, plan, _ := statusPerformanceFixture(t, 5, 0, 2, 0, false)
	plan.Order[0], plan.Order[4] = plan.Order[4], plan.Order[0]
	report, err := statusReport(plan, StatusRepository{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.NextTaskID == nil || *report.NextTaskID != plan.Order[0] || report.Progress.Done != 2 || report.LastCompletion.Completion.Event != 2 {
		t.Fatalf("report: %+v", report)
	}
	for i, id := range plan.Order {
		if report.Tasks[i].ID != id {
			t.Fatalf("order: %v", report.Tasks)
		}
	}
	report.Tasks[1].Attempts[0].Completion.Commit = "changed"
	if plan.Tasks[1].Attempts[0].Completion.Commit == "changed" {
		t.Fatal("report aliases source plan")
	}
	plan.Order[0] = "missing"
	if _, err := statusReport(plan, StatusRepository{}, false); err == nil {
		t.Fatal("invalid plan accepted")
	}
}

func TestStatusUnchangedRemoteStillRejectsLocalEdit(t *testing.T) {
	p, _, _ := statusPerformanceFixture(t, 4, 2, 0, 0, true)
	ctx := context.Background()
	base, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := p.git.PlanCommit(ctx, "", base.Plan.Team.Base)
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, p.git, "update-ref", "refs/remotes/origin/git-task-plan", commit)
	base.Plan.Team.BaseCommit = commit
	base.Plan.Tasks[0].Title = "manual change outside queue"
	writeStatusPlan(t, p, base.Plan)
	before := planBytes(t, p.git)
	report, err := p.StatusReport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range report.Warnings {
		found = found || warning.Code == "shared_receive_failed" && errors.Is(warning.Cause, task.ErrSharedConflict)
	}
	if !found || report.Tasks[0].Title != "manual change outside queue" || !bytes.Equal(before, planBytes(t, p.git)) {
		t.Fatalf("edit hidden or overwritten: %+v", report)
	}
}

func TestStatusCompletionRecheckUsesFreshGit(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	if _, err := p.Start(ctx, StartOptions{Branch: "work"}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, c, "work.txt", "work\n")
	syncTask(t, p, "task-001")
	mergeBranch(t, c, "work", false)
	completed := syncTask(t, p, "task-001")
	if completed.Status != task.Done {
		t.Fatal("fixture not done")
	}
	base, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := base.Plan
	if _, err := next.SetWarnings("task-001", []task.Warning{{Code: "stale", Message: "stale"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.store.Save(ctx, base, next); err != nil {
		t.Fatal(err)
	}
	before := planBytes(t, c)
	p.checkpoint = func(point string) error {
		if point == "sync-observed" {
			_, err := c.Run(ctx, "update-ref", "refs/heads/main", completed.Attempts[0].BaseCommit)
			return err
		}
		return nil
	}
	if _, err := p.Status(ctx); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale cached proof saved: %v", err)
	}
	if !bytes.Equal(before, planBytes(t, c)) {
		t.Fatal("plan changed after Git race")
	}
	p.checkpoint = nil
	if _, err := p.Status(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item, err := current.Plan.FindID("task-001")
	if err != nil || len(item.Warnings) != 1 || item.Warnings[0].Code != "completion_unreachable" {
		t.Fatalf("new call reused stale cache: %+v %v", item, err)
	}
}

func writeStatusPlan(t *testing.T, p *Plans, plan task.Plan) {
	t.Helper()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.git.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

type countingStorageGuard struct {
	storage.Guard
	checks int
}

func (g *countingStorageGuard) CheckStorage(ctx context.Context) error {
	g.checks++
	return g.Guard.CheckStorage(ctx)
}

func TestStatusNoOpLoadsStorageOnce(t *testing.T) {
	p, _, _ := statusPerformanceFixture(t, 3, 0, 0, 0, false)
	repo, err := p.git.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	guard := &countingStorageGuard{Guard: p.git}
	p.store = storage.New(repo.Root, repo.ExcludePath, guard)
	before := planBytes(t, p.git)
	if _, err := p.StatusReport(context.Background()); err != nil {
		t.Fatal(err)
	}
	if guard.checks != 1 {
		t.Fatalf("no-op storage loads: %d, want 1", guard.checks)
	}
	if !bytes.Equal(before, planBytes(t, p.git)) {
		t.Fatal("no-op rewrote plan")
	}
}
