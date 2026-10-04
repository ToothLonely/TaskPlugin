package task

import (
	"math"
	"testing"
)

func TestMergeKindsWhilePaused(t *testing.T) {
	for _, kind := range []MergeKind{MergeCommit, FastForward} {
		t.Run(string(kind), func(t *testing.T) {
			p := planInState(t, Paused)
			c := Completion{Source: Merge, TargetBranch: "main", Commit: testOID, WorkCommit: testOID, MergeKind: kind, ObservedAt: &testTime}
			changed, err := p.Complete("task-a", "active", c)
			requireChange(t, changed, err)
			before := snapshot(t, p)
			changed, err = p.Complete("task-a", "active", c)
			if changed || err != nil || snapshot(t, p) != before {
				t.Fatal("repeated merge changed history")
			}
			c.Source = Manual
			changed, err = p.Complete("task-a", "active", c)
			if changed || err == nil || snapshot(t, p) != before {
				t.Fatal("conflicting event rewrote history")
			}
		})
	}
}

func TestHistoryAndEventValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Plan)
	}{
		{"duplicate event", func(p *Plan) { p.Tasks[1].Attempts[0].Completion.Event = 1 }},
		{"duplicate attempt", func(p *Plan) { p.Tasks[1].Attempts[0].ID = "one" }},
		{"unknown source", func(p *Plan) { p.Tasks[0].Attempts[0].Completion.Source = "squash" }},
		{"lost tail", func(p *Plan) { p.InsertionTail = "" }},
		{"wrong last event", func(p *Plan) { p.LastEvent = 3 }},
		{"completed status mismatch", func(p *Plan) {
			p.Tasks[0].Attempts = append(p.Tasks[0].Attempts, p.Tasks[1].Attempts[0])
			p.Tasks[0].Attempts[0], p.Tasks[0].Attempts[1] = p.Tasks[0].Attempts[1], p.Tasks[0].Attempts[0]
			p.Tasks[1].Attempts = nil
			p.Tasks[1].Status = Todo
			p.Tasks[0].Attempts[0].Status = Paused
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := planForTest(t)
			changed, err := p.CompleteManual("task-a", "one", "", testTime)
			requireChange(t, changed, err)
			changed, err = p.CompleteManual("task-b", "two", "", testTime)
			requireChange(t, changed, err)
			tc.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("accepted invalid history")
			}
		})
	}
}

func TestEventOverflowAndWrongAttempt(t *testing.T) {
	p := planInState(t, Done)
	p.LastEvent = math.MaxUint64
	p.Tasks[0].Attempts[0].Completion.Event = math.MaxUint64
	before := snapshot(t, p)
	changed, err := p.CompleteManual("task-b", "new", "", testTime)
	if changed || err == nil || snapshot(t, p) != before {
		t.Fatal("overflow changed plan")
	}
	p = planInState(t, Active)
	before = snapshot(t, p)
	changed, err = p.Complete("task-a", "wrong", Completion{Source: Manual, TargetBranch: "main"})
	if changed || err == nil || snapshot(t, p) != before {
		t.Fatal("wrong attempt changed plan")
	}
}
