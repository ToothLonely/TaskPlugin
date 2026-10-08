package task

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
var testOID = strings.Repeat("a", 40)

func planForTest(t *testing.T) Plan {
	t.Helper()
	p, err := NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	p.Tasks = []Task{
		{ID: "task-a", Title: "Профиль", Status: Todo},
		{ID: "task-b", Title: " Вход ", Status: Todo},
	}
	p.Order = []string{"task-a", "task-b"}
	return p
}

func attempt(id, branch string) Attempt {
	at := testTime
	return Attempt{ID: id, Branch: branch, OriginalBranch: branch, TargetBranch: "main", BaseCommit: testOID, StartedAt: &at}
}

func requireChange(t *testing.T, changed bool, err error) {
	t.Helper()
	if err != nil || !changed {
		t.Fatalf("changed=%v, err=%v; want true, nil", changed, err)
	}
}

func TestLifecycleAndDelayedCompletion(t *testing.T) {
	p := planForTest(t)
	a := attempt("attempt-1", "feature")
	changed, err := p.Start("task-a", a, false)
	requireChange(t, changed, err)
	*a.StartedAt = testTime.Add(time.Hour)
	if !p.Tasks[0].ActiveAttempt.StartedAt.Equal(testTime) {
		t.Fatal("Start retained caller's date pointer")
	}
	changed, err = p.Pause("task-a")
	requireChange(t, changed, err)
	before := p.Tasks[0].clone()
	changed, err = p.Pause("task-a")
	if changed || err != nil {
		t.Fatalf("repeat pause: %v %v", changed, err)
	}
	changed, err = p.Resume("task-a")
	requireChange(t, changed, err)
	before.ActiveAttempt.Status = Active
	if !reflect.DeepEqual(before.ActiveAttempt, p.Tasks[0].ActiveAttempt) || len(p.Tasks[0].Attempts) != 1 {
		t.Fatal("pause/resume changed attempt")
	}
	c := Completion{Source: Merge, TargetBranch: "main", MergeKind: FastForward, Commit: testOID, WorkCommit: strings.Repeat("b", 40), ObservedAt: &testTime}
	changed, err = p.Complete("task-a", "attempt-1", c)
	requireChange(t, changed, err)
	if p.Tasks[0].Status != Done || p.Tasks[0].ActiveAttempt != nil || len(p.Tasks[0].Attempts) != 1 || p.LastEvent != 1 {
		t.Fatalf("completion: %+v", p)
	}
	history := p.Tasks[0].clone().Attempts
	changed, err = p.Start("task-a", attempt("attempt-2", "feature-2"), false)
	if changed || !errors.Is(err, ErrAgainRequired) {
		t.Fatalf("without --again: %v %v", changed, err)
	}
	changed, err = p.Start("task-a", attempt("attempt-2", "feature-2"), true)
	requireChange(t, changed, err)
	if !reflect.DeepEqual(history, p.Tasks[0].Attempts[:len(history)]) {
		t.Fatal("restart altered history")
	}
	rev := p.Revision
	changed, err = p.Complete("task-a", "attempt-1", c)
	if changed || err != nil || p.Revision != rev || p.Tasks[0].Status != Active {
		t.Fatalf("delayed event changed new attempt: %v %v", changed, err)
	}
	// The same SHA is not an event identity: a different attempt still completes.
	changed, err = p.Complete("task-a", "attempt-2", c)
	requireChange(t, changed, err)
	if len(p.Tasks[0].Attempts) != 2 || p.LastEvent != 2 {
		t.Fatal("new attempt not registered")
	}
}

func TestTransitionMatrix(t *testing.T) {
	type action struct {
		name    string
		run     func(*Plan) (bool, error)
		allowed map[Status]bool
		noops   map[Status]bool
	}
	actions := []action{
		{"start", func(p *Plan) (bool, error) { return p.Start("task-a", attempt("new", "new-branch"), false) }, map[Status]bool{Todo: true, Active: true, Paused: true}, nil},
		{"again", func(p *Plan) (bool, error) { return p.Start("task-a", attempt("new", "new-branch"), true) }, map[Status]bool{Done: true}, nil},
		{"attach", func(p *Plan) (bool, error) { return p.Attach("task-a", attempt("new", "new-branch")) }, map[Status]bool{Todo: true, Active: true, Paused: true}, nil},
		{"pause", func(p *Plan) (bool, error) { return p.Pause("task-a") }, map[Status]bool{Active: true, Paused: true}, map[Status]bool{Paused: true}},
		{"resume", func(p *Plan) (bool, error) { return p.Resume("task-a") }, map[Status]bool{Paused: true}, nil},
		{"archive", func(p *Plan) (bool, error) { return p.Archive("task-a") }, map[Status]bool{Todo: true, Active: true, Paused: true, Done: true, Archived: true}, map[Status]bool{Archived: true}},
		{"manual", func(p *Plan) (bool, error) {
			id := ""
			if p.Tasks[0].Status == Todo {
				id = "manual-1"
			}
			return p.CompleteManual("task-a", id, "", testTime)
		}, map[Status]bool{Todo: true, Active: true, Paused: true, Done: true}, map[Status]bool{Done: true}},
		{"rebind", func(p *Plan) (bool, error) { return p.Rebind("task-a", "replacement", testOID, testTime) }, map[Status]bool{Active: true, Paused: true}, nil},
	}
	for _, status := range []Status{Todo, Active, Paused, Done, Archived} {
		for _, act := range actions {
			t.Run(string(status)+"/"+act.name, func(t *testing.T) {
				p := planInState(t, status)
				before := snapshot(t, p)
				changed, err := act.run(&p)
				if act.allowed[status] {
					if err != nil || changed == act.noops[status] {
						t.Fatalf("changed=%v err=%v", changed, err)
					}
				} else if err == nil || changed {
					t.Fatalf("forbidden transition: changed=%v err=%v", changed, err)
				}
				if !changed && before != snapshot(t, p) {
					t.Fatal("failed/no-op transition changed plan")
				}
			})
		}
	}
}

func planInState(t *testing.T, status Status) Plan {
	t.Helper()
	p := planForTest(t)
	if status == Active || status == Paused {
		changed, err := p.Start("task-a", attempt("active", "feature"), false)
		requireChange(t, changed, err)
	}
	if status == Paused {
		changed, err := p.Pause("task-a")
		requireChange(t, changed, err)
	}
	if status == Done {
		changed, err := p.CompleteManual("task-a", "manual", "", testTime)
		requireChange(t, changed, err)
	}
	if status == Archived {
		changed, err := p.Archive("task-a")
		requireChange(t, changed, err)
	}
	return p
}

func TestBindingArchiveAndRebind(t *testing.T) {
	p := planInState(t, Paused)
	before := snapshot(t, p)
	changed, err := p.Attach("task-b", attempt("second", "feature"))
	if changed || !errors.Is(err, ErrBranchInUse) || before != snapshot(t, p) {
		t.Fatalf("paused binding conflict: %v %v", changed, err)
	}
	changed, err = p.Rebind("task-a", "other", strings.Repeat("b", 64), testTime)
	requireChange(t, changed, err)
	a := p.Tasks[0].ActiveAttempt
	if a.ID != "active" || a.OriginalBranch != "feature" || len(a.Rebindings) != 1 || a.Branch != "other" || p.Tasks[0].Status != Paused {
		t.Fatalf("rebind lost history: %+v", a)
	}
	changed, err = p.Rebind("task-a", "other", testOID, testTime)
	requireChange(t, changed, err)
	a = p.Tasks[0].ActiveAttempt
	if len(a.Rebindings) != 2 || a.Rebindings[1].From != "other" || a.Rebindings[1].To != "other" || a.BaseCommit != testOID || a.ID != "active" || p.Tasks[0].Status != Paused {
		t.Fatalf("same-name renewal lost binding history: %+v", a)
	}
	changed, err = p.Archive("task-a")
	requireChange(t, changed, err)
	changed, err = p.Attach("task-b", attempt("second", "other"))
	requireChange(t, changed, err)
	if p.Tasks[0].ActiveAttempt == nil || len(p.Tasks[0].Attempts) != 1 {
		t.Fatal("archive fabricated completion or lost snapshot")
	}
}

func TestCompletionSourcesAndEventOrder(t *testing.T) {
	p := planForTest(t)
	changed, err := p.Complete("task-b", "imported", Completion{Source: Imported, TargetBranch: "main"})
	requireChange(t, changed, err)
	a := p.Tasks[1].Attempts[0]
	if a.StartedAt != nil || a.Branch != "" || a.Completion.CompletedAt != nil || a.Completion.ObservedAt != nil {
		t.Fatal("import invented facts")
	}
	changed, err = p.CompleteManual("task-a", "manual", "", testTime)
	requireChange(t, changed, err)
	if p.LastEvent != 2 || p.InsertionTail != "task-a" || p.Tasks[0].Attempts[0].Completion.Source != Manual {
		t.Fatal("registration order/source lost")
	}
	before := snapshot(t, p)
	changed, err = p.CompleteManual("task-a", "unused", "", testTime.Add(time.Hour))
	if changed || err != nil || snapshot(t, p) != before {
		t.Fatal("repeat manual complete changed plan")
	}
	changed, err = p.CompleteManual("task-a", "unused", testOID, testTime)
	if changed || !errors.Is(err, ErrTransition) || snapshot(t, p) != before {
		t.Fatal("done accepted new evidence")
	}
	changed, err = p.Start("task-b", attempt("again", "branch"), true)
	requireChange(t, changed, err)
	changed, err = p.CompleteManual("task-b", "", testOID, testTime.Add(-time.Hour))
	requireChange(t, changed, err)
	if p.LastEvent != 3 || p.Tasks[1].Attempts[1].ID != "again" {
		t.Fatal("clock rollback affected event order")
	}
}

func TestFailedTransitionsAreAtomic(t *testing.T) {
	cases := []struct {
		name string
		run  func(*Plan) (bool, error)
	}{
		{"invalid branch", func(p *Plan) (bool, error) { return p.Start("task-a", attempt("new", "--force"), true) }},
		{"target branch", func(p *Plan) (bool, error) { return p.Start("task-a", attempt("new", "main"), true) }},
		{"reused attempt", func(p *Plan) (bool, error) { return p.Start("task-a", attempt("manual", "branch"), true) }},
		{"missing task", func(p *Plan) (bool, error) { return p.Archive("absent") }},
		{"revision overflow", func(p *Plan) (bool, error) { return p.Archive("task-a") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := planInState(t, Done)
			if tc.name == "revision overflow" {
				p.Revision = math.MaxUint64
			}
			before := snapshot(t, p)
			changed, err := tc.run(&p)
			if changed || err == nil || before != snapshot(t, p) {
				t.Fatalf("not atomic: %v %v", changed, err)
			}
		})
	}
}

func TestWarningsAndSnapshots(t *testing.T) {
	p := planInState(t, Active)
	warnings := []Warning{{Code: "branch_missing", Message: "Ветка удалена"}}
	changed, err := p.SetWarnings("task-a", warnings)
	requireChange(t, changed, err)
	warnings[0].Code = "changed"
	if p.Tasks[0].Status != Active || p.Tasks[0].ActiveAttempt == nil || p.Tasks[0].Warnings[0].Code != "branch_missing" {
		t.Fatal("warning changed state or aliases input")
	}
	saved, err := p.FindID("task-a")
	if err != nil {
		t.Fatal(err)
	}
	saved.ActiveAttempt.Branch = "changed"
	*saved.ActiveAttempt.StartedAt = testTime.Add(time.Hour)
	saved.Warnings[0].Code = "changed"
	if p.Tasks[0].ActiveAttempt.Branch != "feature" || !p.Tasks[0].ActiveAttempt.StartedAt.Equal(testTime) || p.Tasks[0].Warnings[0].Code != "branch_missing" {
		t.Fatal("FindID leaks mutable data")
	}
}

func TestSelectionAndStableIdentity(t *testing.T) {
	p := planForTest(t)
	p.Order = []string{"task-b", "task-a"}
	got, err := p.FirstTodo()
	if err != nil || got.ID != "task-b" {
		t.Fatalf("first todo: %+v %v", got, err)
	}
	p.Tasks[0].Title = p.Tasks[1].Title
	_, err = p.FindTitle(" Вход ")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatal(err)
	}
	changed, err := p.Archive("task-a")
	requireChange(t, changed, err)
	_, err = p.FindTitle(" Вход ")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatal("archived duplicate ignored")
	}
	for _, id := range []string{"1", "missing-id", "TASK-A"} {
		if _, err := p.FindID(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ID alias accepted: %s", id)
		}
	}
	p.Tasks[0].Title = "Новое имя"
	got, err = p.FindTitle("Новое имя")
	if err != nil || got.ID != "task-a" {
		t.Fatal("rename changed identity")
	}
	for _, title := range []string{"вход", "Вход", " Вход", "Absent"} {
		if _, err := p.FindTitle(title); !errors.Is(err, ErrNotFound) {
			t.Fatalf("title normalized: %q", title)
		}
	}
	changed, err = p.Archive("task-b")
	requireChange(t, changed, err)
	if _, err := p.FirstTodo(); !errors.Is(err, ErrNotFound) {
		t.Fatal("no todo should fail")
	}
}

func TestAgainThroughBothSelectors(t *testing.T) {
	for _, selector := range []string{"id", "title"} {
		t.Run(selector, func(t *testing.T) {
			p := planInState(t, Done)
			selected, err := p.FindID("task-a")
			if selector == "title" {
				selected, err = p.FindTitle("Профиль")
			}
			if err != nil {
				t.Fatal(err)
			}
			changed, err := p.Start(selected.ID, attempt("retry", "retry-branch"), true)
			requireChange(t, changed, err)
			if p.Tasks[0].Status != Active || len(p.Tasks[0].Attempts) != 2 {
				t.Fatal("selector changed restart semantics")
			}
			first, err := p.FirstTodo()
			if err != nil || first.ID != "task-b" {
				t.Fatalf("first todo after restart: %+v %v", first, err)
			}
		})
	}
}
