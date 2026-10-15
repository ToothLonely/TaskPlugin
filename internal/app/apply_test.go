package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"git-task/internal/git"
	"git-task/internal/task"
)

func writeManualTasks(t *testing.T, c *git.Client) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(planBytes(t, c), &fields); err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(fields["tasks"], &tasks); err != nil {
		t.Fatal(err)
	}
	tasks[0]["title"] = json.RawMessage(`"Edited title"`)
	tasks[0]["description"] = json.RawMessage(`"Edited description"`)
	tasks = append(tasks, map[string]json.RawMessage{"title": json.RawMessage(`"New task"`)}, map[string]json.RawMessage{"title": json.RawMessage(`"Manual ID"`), "id": json.RawMessage(`"manual-id"`)})
	fields["tasks"], _ = json.Marshal(tasks)
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestApplyWorkingPlanAfterStartAndCompletion(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	started, err := p.Start(ctx, StartOptions{Branch: "feature/finished"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.PrepareComplete(ctx, started.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	done, err := p.ApplyComplete(ctx, preview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Start(ctx, StartOptions{Branch: "feature/active"}); err != nil {
		t.Fatal(err)
	}
	before, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writeManualTasks(t, c)
	if result, _, err := p.InitTemplate(ctx, true); err != nil || !result.Applied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	shown, _, err := p.Show(ctx, done.ID)
	if err != nil || shown.Status != task.Done || shown.Title != "Edited title" || !reflect.DeepEqual(shown.Attempts, done.Attempts) {
		t.Fatalf("show: %+v %v", shown, err)
	}
	after, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, previous := range before.Plan.Tasks[1:] {
		current, err := after.Plan.FindID(previous.ID)
		if err != nil || !reflect.DeepEqual(previous, current) {
			t.Fatalf("history changed: %+v %v", current, err)
		}
	}
	if _, err := p.StatusReport(ctx); err != nil {
		t.Fatal(err)
	}
	added, err := p.Add(ctx, "Later", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	after, err = p.store.Load(ctx)
	if err != nil || after.Plan.Order[len(after.Plan.Order)-1] != added.ID {
		t.Fatalf("order: %v %v", after.Plan.Order, err)
	}
}

func TestApplyTeamPublishesOfflineAndRetriesWithoutDuplicates(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	writeManualTasks(t, f.ca)
	command := f.a.ForCommand()
	if result, _, err := command.InitTemplate(ctx, true); err != nil || !result.Applied || result.ActionID == "" {
		t.Fatalf("apply: %+v %v", result, err)
	}
	base, err := f.a.store.Load(ctx)
	if err != nil || len(base.Plan.Team.Pending) != 1 {
		t.Fatalf("queue: %+v %v", base.Plan.Team, err)
	}
	if err := checkQueue(base.Plan); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := command.PublishRecordedActions(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("offline: %v", err)
	}
	ready := planBytes(t, f.ca)
	if result, _, err := f.a.InitTemplate(ctx, true); err != nil || result.Applied || !bytes.Equal(ready, planBytes(t, f.ca)) {
		t.Fatalf("repeat: %+v %v", result, err)
	}
	lost := errors.New("lost acknowledgement")
	command.checkpoint = func(point string) error {
		if point == "publish-after-push" {
			return lost
		}
		return nil
	}
	if err := command.PublishRecordedActions(ctx); !errors.Is(err, lost) {
		t.Fatalf("lost acknowledgement: %v", err)
	}
	command.checkpoint = nil
	if _, _, err := f.a.InitTemplate(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := command.PublishRecordedActions(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.b.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	remote, err := f.b.store.Load(ctx)
	if err != nil || len(remote.Plan.Tasks) != 3 || len(remote.Plan.Actions) != len(base.Plan.Actions)+1 {
		t.Fatalf("remote: %+v %v", remote.Plan, err)
	}
	got, err := remote.Plan.FindID(f.id)
	if err != nil || got.Title != "Edited title" {
		t.Fatalf("text not shared: %+v %v", got, err)
	}
	stable := planBytes(t, f.ca)
	if _, _, err := f.a.InitTemplate(ctx, true); err != nil || !bytes.Equal(stable, planBytes(t, f.ca)) {
		t.Fatalf("published repeat: %v", err)
	}
}

func TestApplyTeamTextOnlyAndReorderedTasks(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Add(ctx, "Second", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	base, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan := base.Plan
	plan.Tasks[0].Title = "Only text changed"
	plan.Tasks[0], plan.Tasks[1] = plan.Tasks[1], plan.Tasks[0]
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ca.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	command := f.a.ForCommand()
	result, _, err := command.InitTemplate(ctx, true)
	if err != nil || !result.Applied || result.ActionID == "" {
		t.Fatalf("text apply: %+v %v", result, err)
	}
	ready := planBytes(t, f.ca)
	if result, _, err := f.a.InitTemplate(ctx, true); err != nil || result.Applied || !bytes.Equal(ready, planBytes(t, f.ca)) {
		t.Fatalf("reordered repeat: %+v %v", result, err)
	}
	if err := command.PublishRecordedActions(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.b.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	shown, _, err := f.b.Show(ctx, f.id)
	if err != nil || shown.Title != "Only text changed" {
		t.Fatalf("shared text: %+v %v", shown, err)
	}
}
