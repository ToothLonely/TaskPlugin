package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-task/internal/app"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestStatusShowArgumentsAndHelp(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--json", "--json"}, {"status", "--json=true"}, {"status", "--help", "extra"},
		{"status", "--", "--json"}, {"status", "--id=x"}, {"status", "--help", "--help"},
		{"show"}, {"show", "--id"}, {"show", "--id="}, {"show", "--id=x", "--id=x"},
		{"show", "--id=x", "extra"}, {"show", "--id=x", "--json"}, {"show", "--help", "--help"},
		{"show", "--title=x"}, {"show", "--", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) {
				t.Fatal("opened repository on argument error")
				return nil, nil
			})
			if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
				t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, &out, &diagnostic)
			}
		})
	}
	t.Setenv("PATH", "")
	for _, args := range [][]string{{"status", "--json", "--help"}, {"status", "--help", "--"}, {"show", "--help"}, {"help", "show"}, {"help", "status"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}); code != 0 || !strings.Contains(out.String(), "Использование:") || diagnostic.Len() != 0 {
			t.Fatalf("help %v: %d %q %q", args, code, &out, &diagnostic)
		}
	}
}

func TestStatusJSONSingleObjectWarningsAndReadOnlyShow(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	started, err := p.Start(ctx, app.StartOptions{Branch: "lost", ID: "task-001"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "checkout", "main")
	testrepo.Run(t, c, "branch", "-D", "lost")
	var out, diagnostic bytes.Buffer
	if code := RunWithPlans(ctx, []string{"status", "--json"}, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 {
		t.Fatalf("status: %d %q", code, &diagnostic)
	}
	decoder := json.NewDecoder(&out)
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		t.Fatalf("extra stdout after JSON: %v", err)
	}
	for _, name := range []string{"schema_version", "repository", "progress", "tasks", "current_task_id", "last_completion", "next_task_id", "warnings"} {
		if _, ok := raw[name]; !ok {
			t.Fatalf("missing field %s", name)
		}
	}
	if string(raw["schema_version"]) != "3" || string(raw["current_task_id"]) != "null" || string(raw["last_completion"]) != "null" || string(raw["next_task_id"]) != `"task-002"` || !strings.Contains(diagnostic.String(), "branch_missing") {
		t.Fatalf("schema/diagnostics: %v %q", raw, &diagnostic)
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tasks"], &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 || tasks[0]["active_attempt"] != nil || tasks[0]["attempts"] == nil || tasks[1]["active_attempt"] != nil || tasks[1]["attempts"] != nil {
		t.Fatalf("attempt separation before first done: %v", tasks)
	}
	before := selectionBytes(t, c)
	out.Reset()
	diagnostic.Reset()
	if code := RunWithPlans(ctx, []string{"status", "--json", "--"}, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 || !bytes.Equal(before, selectionBytes(t, c)) || bytes.Contains(out.Bytes(), []byte{27}) || !json.Valid(out.Bytes()) {
		t.Fatalf("repeat: %d %q %q", code, &out, &diagnostic)
	}
	out.Reset()
	diagnostic.Reset()
	if code := RunWithPlans(ctx, []string{"show", "--id=" + started.ID}, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 || !strings.Contains(out.String(), "Подходы:") || !strings.Contains(out.String(), "lost") || !strings.Contains(out.String(), "статус active; автор") || !strings.Contains(out.String(), started.ActiveAttempt.ID) || !strings.Contains(out.String(), "branch_missing") || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("show: %d %q %q", code, &out, &diagnostic)
	}
	out.Reset()
	if code := RunWithPlans(ctx, []string{"status", "--json"}, "test", Streams{Out: &out, Err: failedWriter{}}, open); code != 1 || out.Len() != 0 {
		t.Fatalf("diagnostic failure emitted JSON: %d %q", code, &out)
	}
	for _, args := range [][]string{{"show", "--id=absent"}, {"status", "--json"}} {
		if args[0] == "status" {
			if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		out.Reset()
		diagnostic.Reset()
		if code := RunWithPlans(ctx, args, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 1 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("unreliable/missing snapshot: %d %q %q", code, &out, &diagnostic)
		}
	}
}

func TestStatusAndShowOutputFailures(t *testing.T) {
	p, _ := selectionFixture(t)
	ctx := context.Background()
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	for _, args := range [][]string{{"status"}, {"status", "--json"}, {"show", "--id=task-001"}} {
		var diagnostic bytes.Buffer
		if code := RunWithPlans(ctx, args, "test", Streams{Out: failedWriter{}, Err: &diagnostic}, open); code != 1 || diagnostic.Len() == 0 {
			t.Fatalf("write failure %v: %d %q", args, code, &diagnostic)
		}
	}
}

func TestStatusDisplayContextAndHistoricalAttemptLabels(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	percent := 33.3
	branch := "second"
	current := "b"
	next := "c"
	attempt := task.Attempt{ID: "attempt-b", Branch: "second", OriginalBranch: "old", TargetBranch: "main", BaseCommit: strings.Repeat("a", 40), StartedAt: &at, Rebindings: []task.Rebinding{{From: "old", To: "second", BaseCommit: strings.Repeat("b", 40), ObservedAt: at}}}
	c := task.Completion{Event: 2, Source: task.Imported, TargetBranch: "main", ObservedAt: &at}
	r := app.StatusReport{
		Repository: app.StatusRepository{TargetBranch: "main", CurrentBranch: &branch, HeadCommittedAt: &at, HeadCommit: &attempt.BaseCommit},
		Progress:   app.Progress{Done: 1, Total: 3, Percent: &percent}, CurrentTaskID: &current, NextTaskID: &next,
		LastCompletion: &app.LastCompletion{TaskID: "b", TaskStatus: task.Active, AttemptID: "previous", Completion: c},
		Tasks: []task.Task{
			{ID: "a", Title: "First", Status: task.Paused, Attempts: []task.Attempt{{Status: task.Paused, Branch: "first", TargetBranch: "main"}}},
			{ID: "b", Title: "Second", Description: "Remember\ncontext\x1b", Status: task.Active, Attempts: []task.Attempt{{ID: "previous", Status: task.Done, Completion: &c}, attempt}},
			{ID: "c", Title: "Next", Status: task.Todo},
		},
	}
	var out bytes.Buffer
	if err := writeStatusReport(&out, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"33.3%", "ветка: first", "ветка: second", "Remember context", "текущий статус active", "источник imported", "первый todo", "время Git-коммита", "не время последней работы человека", "Известное время завершения: неизвестно"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, &out)
		}
	}
	activeStart := strings.Index(out.String(), "Активные и приостановленные задачи:\n")
	activeEnd := strings.Index(out.String(), "Последнее зарегистрированное завершение:")
	if activeStart < 0 || activeEnd < activeStart {
		t.Fatalf("missing active task section: %q", &out)
	}
	activeText := out.String()[activeStart:activeEnd]
	first := strings.Index(activeText, "ветка: first")
	second := strings.Index(activeText, "ветка: second")
	if first < 0 || second < 0 || first > second || bytes.Contains(out.Bytes(), []byte{27}) {
		t.Fatalf("unstable order or terminal control: %q", &out)
	}
	var show strings.Builder
	writeAttempt(&show, attempt)
	writeAttempt(&show, task.Attempt{ID: "previous", Completion: &c})
	for _, want := range []string{"исходная ветка: old", "Перепривязка: old -> second", "источник imported", "Commit основания", "Известное время завершения: неизвестно", "зарегистрировано наблюдение: 2026-10-01"} {
		if !strings.Contains(show.String(), want) {
			t.Fatalf("missing attempt data %q: %s", want, &show)
		}
	}
}
