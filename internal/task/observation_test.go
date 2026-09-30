package task

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestObservationValidationIsolationAndRebind(t *testing.T) {
	p := planForTest(t)
	if _, err := p.Start("task-a", attempt("current", "feature"), false); err != nil {
		t.Fatal(err)
	}
	o := &Observation{Tip: testOID, TargetCommit: testOID, WorkCommit: strings.Repeat("b", 40), BranchLog: strings.Repeat("c", 64), TargetLog: strings.Repeat("d", 64)}
	if changed, err := p.Observe("task-a", "current", o, nil); err != nil || !changed {
		t.Fatalf("observe: %v %v", changed, err)
	}
	before := cloneObservationPlan(t, p)
	if changed, err := p.Observe("task-a", "current", o, nil); err != nil || changed || !reflect.DeepEqual(p, before) {
		t.Fatalf("no-op: %v %v", changed, err)
	}
	if _, err := p.Observe("task-a", "other", o, nil); !errors.Is(err, ErrTransition) {
		t.Fatalf("wrong attempt: %v", err)
	}
	o.Tip = strings.Repeat("e", 40)
	item, _ := p.FindID("task-a")
	item.ActiveAttempt.Observation.WorkCommit = ""
	if !reflect.DeepEqual(p, before) {
		t.Fatal("mutable observation alias")
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var copy Plan
	if err := json.Unmarshal(data, &copy); err != nil || !reflect.DeepEqual(p, copy) {
		t.Fatalf("round-trip: %v", err)
	}
	for _, edit := range []func(*Observation){
		func(o *Observation) { o.Tip = "short" },
		func(o *Observation) { o.TargetCommit = "" },
		func(o *Observation) { o.BranchLog = "" },
		func(o *Observation) { o.TargetLog = "invalid" },
		func(o *Observation) { o.WorkCommit = "invalid" },
	} {
		bad := cloneObservationPlan(t, p)
		edit(bad.Tasks[0].ActiveAttempt.Observation)
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad observation accepted: %v", err)
		}
	}
	if _, err := p.Rebind("task-a", "new-feature", testOID, testTime); err != nil || p.Tasks[0].ActiveAttempt.Observation != nil {
		t.Fatalf("rebind retained evidence: %v", err)
	}
}

func cloneObservationPlan(t *testing.T, p Plan) Plan {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var copy Plan
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestSameNameRebindPreservesCompletedHistory(t *testing.T) {
	p := planInState(t, Done)
	if _, err := p.Start("task-a", attempt("again", "feature"), true); err != nil {
		t.Fatal(err)
	}
	o := &Observation{Tip: testOID, TargetCommit: testOID, WorkCommit: strings.Repeat("b", 40), BranchLog: strings.Repeat("c", 64), TargetLog: strings.Repeat("d", 64)}
	if _, err := p.Observe("task-a", "again", o, []Warning{{Code: "binding_uncertain", Message: "branch recreated"}}); err != nil {
		t.Fatal(err)
	}
	before := cloneObservationPlan(t, p)
	if changed, err := p.Rebind("task-a", "feature", testOID, testTime); err != nil || !changed {
		t.Fatalf("same-name renewal: %v %v", changed, err)
	}
	task := p.Tasks[0]
	a := task.ActiveAttempt
	if task.Status != Active || a.ID != "again" || a.OriginalBranch != "feature" || a.BaseCommit != testOID || a.Observation != nil || len(task.Warnings) != 0 || len(a.Rebindings) != 1 || a.Rebindings[0].From != "feature" || a.Rebindings[0].To != "feature" || !reflect.DeepEqual(task.Attempts, before.Tasks[0].Attempts) {
		t.Fatalf("renewal changed identity or completed history: %+v", task)
	}
	if copy := cloneObservationPlan(t, p); !reflect.DeepEqual(copy, p) {
		t.Fatal("same-name history lost in JSON round-trip")
	}
	for _, edit := range []func(*Attempt){
		func(a *Attempt) { a.Rebindings[0].From = "other" },
		func(a *Attempt) { a.Rebindings[0].To = "main" },
		func(a *Attempt) { a.Rebindings[0].BaseCommit = "short" },
		func(a *Attempt) { a.BaseCommit = strings.Repeat("e", 40) },
	} {
		bad := cloneObservationPlan(t, p)
		edit(bad.Tasks[0].ActiveAttempt)
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid same-name history accepted: %v", err)
		}
	}
}
