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

func TestStatusReportMultipleTasksHistoryAndNoOp(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	for _, title := range []string{"Завершённая", "Архивная"} {
		if _, err := addFixtureTask(t, p, ctx, title, "контекст", task.Position{End: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, start := range []StartOptions{{Branch: "first", ID: "task-001"}, {Branch: "paused", ID: "task-002"}, {Branch: "frozen", ID: "task-005"}} {
		if _, err := p.Start(ctx, start); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.Pause(ctx, "task-002"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Archive(ctx, "task-005"); err != nil {
		t.Fatal(err)
	}
	preview, err := p.PrepareComplete(ctx, "task-004", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyComplete(ctx, preview); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "branch", "-D", "paused")
	report, err := p.StatusReport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentTaskID != nil || len(report.Tasks) != 5 || report.Progress.Done != 1 || report.Progress.Total != 4 || report.Progress.Percent == nil || *report.Progress.Percent != 25 {
		t.Fatalf("progress and archived binding: %+v", report)
	}
	if report.Tasks[0].Status != task.Active || report.Tasks[1].Status != task.Paused || report.Tasks[4].Status != task.Archived || report.NextTaskID == nil || *report.NextTaskID != "task-003" || report.LastCompletion == nil || report.LastCompletion.TaskID != "task-004" {
		t.Fatalf("task selection: %+v", report)
	}
	missing := false
	for _, w := range report.Warnings {
		missing = missing || w.Code == "branch_missing" && w.TaskID == "task-002"
	}
	if !missing {
		t.Fatalf("missing branch warning: %+v", report.Warnings)
	}
	before := planBytes(t, c)
	backupPath := filepath.Join(c.Dir, ".git-task", "plan.backup.json")
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(c.Dir, ".git-task", "plan.json")
	info, err := os.Stat(planPath)
	if err != nil {
		t.Fatal(err)
	}
	refs := testrepo.Run(t, c, "show-ref")
	codeState := testrepo.Run(t, c, "status", "--porcelain=v1", "-z")
	if _, err := p.StatusReport(ctx); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(planPath)
	if err != nil {
		t.Fatal(err)
	}
	afterBackup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, planBytes(t, c)) || !bytes.Equal(backup, afterBackup) || !info.ModTime().Equal(afterInfo.ModTime()) || !bytes.Equal(refs, testrepo.Run(t, c, "show-ref")) || !bytes.Equal(codeState, testrepo.Run(t, c, "status", "--porcelain=v1", "-z")) {
		t.Fatal("unchanged status changed storage, refs or code")
	}
	testrepo.Run(t, c, "checkout", "first")
	report, err = p.StatusReport(ctx)
	if err != nil || report.CurrentTaskID == nil || *report.CurrentTaskID != "task-001" || report.Repository.HeadCommittedAt == nil {
		t.Fatalf("current task: %+v %v", report, err)
	}
	if _, err := p.Start(ctx, StartOptions{Branch: "again", ID: "task-004", Again: true}); err != nil {
		t.Fatal(err)
	}
	report, err = p.StatusReport(ctx)
	if err != nil || report.Progress.Done != 0 || report.LastCompletion.TaskStatus != task.Active || len(report.Tasks[3].Attempts) != 2 || report.Tasks[3].ActiveAttempt == nil {
		t.Fatalf("again hides historical completion: %+v %v", report, err)
	}
	if _, _, err := p.Archive(ctx, "task-004"); err != nil {
		t.Fatal(err)
	}
	report, err = p.StatusReport(ctx)
	if err != nil || report.Progress.Total != 3 || report.LastCompletion.TaskStatus != task.Archived || report.CurrentTaskID != nil {
		t.Fatalf("archived history: %+v %v", report, err)
	}
	testrepo.Run(t, c, "checkout", "--detach")
	report, err = p.StatusReport(ctx)
	if err != nil || !report.Repository.Detached || report.Repository.CurrentBranch != nil || report.CurrentTaskID != nil || report.Repository.HeadCommit == nil {
		t.Fatalf("detached: %+v %v", report, err)
	}
}

func TestStatusReportUnbornEmptyAndOnlyArchived(t *testing.T) {
	t.Parallel()
	c := testrepo.New(t)
	ctx := context.Background()
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plans{git: c, store: storage.New(repo.Root, repo.ExcludePath, c)}
	if _, _, err := p.Init(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	for _, archive := range []bool{false, true} {
		if archive {
			added, err := addFixtureTask(t, p, ctx, "Архивная", "", task.Position{})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := p.Archive(ctx, added.ID); err != nil {
				t.Fatal(err)
			}
		}
		before := planBytes(t, c)
		r, err := p.StatusReport(ctx)
		if err != nil || !r.Repository.Unborn || r.Repository.Detached || r.Repository.CurrentBranch == nil || *r.Repository.CurrentBranch != "main" || r.Repository.HeadCommit != nil || r.Repository.HeadCommittedAt != nil || r.Progress.Total != 0 || r.Progress.Percent != nil || r.CurrentTaskID != nil || r.NextTaskID != nil || r.LastCompletion != nil || r.Tasks == nil || r.Warnings == nil {
			t.Fatalf("empty/archived: %+v %v", r, err)
		}
		if !bytes.Equal(before, planBytes(t, c)) {
			t.Fatal("read rewrote plan")
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"warnings":[]`, `"percent":null`, `"current_task_id":null`, `"last_completion":null`, `"next_task_id":null`} {
			if !bytes.Contains(data, []byte(want)) {
				t.Fatalf("missing %s in %s", want, data)
			}
		}
		if !archive && !bytes.Contains(data, []byte(`"tasks":[]`)) {
			t.Fatalf("empty tasks are not array: %s", data)
		}
	}
}

func TestShowDoesNotSyncAndStatusCompletesObservedMerge(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "work"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	syncTask(t, p, started.ID)
	mergeBranch(t, c, "work", true)
	before := planBytes(t, c)
	item, pending, err := p.Show(ctx, started.ID)
	if err != nil || pending || item.Status != task.Active || item.ActiveAttempt == nil || len(item.Attempts) != 1 || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("show performed sync: %+v %v", item, err)
	}
	if _, _, err := p.Show(ctx, "missing"); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	r, err := p.StatusReport(ctx)
	if err != nil || r.Progress.Done != 1 || r.LastCompletion == nil || r.LastCompletion.Completion.Source != task.Merge || r.LastCompletion.Completion.CompletedAt != nil || r.Tasks[0].ActiveAttempt != nil || len(r.Tasks[0].Attempts) != 1 {
		t.Fatalf("status completion: %+v %v", r, err)
	}
	if _, _, err := p.Archive(ctx, started.ID); err != nil {
		t.Fatal(err)
	}
	item, _, err = p.Show(ctx, started.ID)
	if err != nil || item.Status != task.Archived || len(item.Attempts) != 1 {
		t.Fatalf("archived show: %+v %v", item, err)
	}
}

func TestStatusAndShowLeavePendingOperationUntouched(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	stop := errors.New("interrupted after checkout")
	p.checkpoint = func(point string) error {
		if point == "checked-out" {
			return stop
		}
		return nil
	}
	ctx := context.Background()
	if _, err := p.Start(ctx, StartOptions{Branch: "pending"}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	p.checkpoint = nil
	before := planBytes(t, c)
	journalPath := filepath.Join(c.Dir, ".git-task", "operation.json")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.StatusReport(ctx)
	if err != nil || r.Tasks[0].Status != task.Todo || r.CurrentTaskID != nil || len(r.Warnings) != 1 || r.Warnings[0].Code != "operation_pending" {
		t.Fatalf("pending status: %+v %v", r, err)
	}
	if item, pending, err := p.Show(ctx, "task-001"); err != nil || !pending || item.Status != task.Todo {
		t.Fatalf("pending show: %+v %v %v", item, pending, err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(journal, afterJournal) || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatalf("pending operation modified: %v", err)
	}
	if head, err := c.HeadState(ctx); err != nil || head.Ref != "refs/heads/pending" {
		t.Fatalf("Git changed: %+v %v", head, err)
	}
}

func TestStatusProgressRoundingAndEventOrder(t *testing.T) {
	plan, err := task.NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"One", "Two", "Three"} {
		if _, err := plan.Add(title, "", task.Position{}); err != nil {
			t.Fatal(err)
		}
	}
	testrepo.FixtureIDs(&plan)
	if _, err := plan.Complete("task-001", "import-one", task.Completion{Source: task.Imported, TargetBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Move("task-003", task.Position{Before: "task-002"}); err != nil {
		t.Fatal(err)
	}
	r, err := statusReport(plan, StatusRepository{TargetBranch: "main"}, false)
	if err != nil || r.Progress.Percent == nil || *r.Progress.Percent != 33.3 || r.NextTaskID == nil || *r.NextTaskID != "task-003" || r.LastCompletion.Completion.CompletedAt != nil {
		t.Fatalf("rounding/order: %+v %v", r, err)
	}
	if _, err := plan.Complete("task-003", "import-three", task.Completion{Source: task.Imported, TargetBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Move("task-001", task.Position{End: true}); err != nil {
		t.Fatal(err)
	}
	r, err = statusReport(plan, StatusRepository{}, false)
	if err != nil || r.Progress.Percent == nil || *r.Progress.Percent != 66.7 || r.LastCompletion.TaskID != "task-003" || r.NextTaskID == nil || *r.NextTaskID != "task-002" {
		t.Fatalf("event order follows plan order: %+v %v", r, err)
	}
}
