package task

import (
	"errors"
	"strings"
	"testing"
)

func TestPartialCompletionWithoutInsertionTail(t *testing.T) {
	for _, mode := range []string{"manual", "tracking", "server"} {
		t.Run(mode, func(t *testing.T) {
			p := planForTest(t)
			for _, a := range []Attempt{attempt("one", "one"), attempt("two", "two")} {
				if _, err := p.Start("task-a", a, false); err != nil {
					t.Fatal(err)
				}
			}
			p.InsertionTail = ""
			c := Completion{Source: Manual, TargetBranch: "main"}
			if mode != "manual" {
				c.Source, c.MergeKind = Merge, FastForward
				c.Commit, c.WorkCommit = strings.Repeat("b", 40), strings.Repeat("c", 40)
				c.ObservedAt = &testTime
			}
			if mode == "server" {
				c.TargetBefore = strings.Repeat("a", 40)
			}
			if _, err := p.Complete("task-a", "one", c); err != nil {
				t.Fatal(err)
			}
			item, _ := p.FindID("task-a")
			if p.InsertionTail != "" || item.Status != Active || item.Attempts[0].Status != Done || item.Attempts[1].Status != Active {
				t.Fatalf("partial completion changed insertion cursor/other approach: %+v", p)
			}
			if _, err := p.CompleteManual("task-a", "two", "", testTime); err != nil {
				t.Fatal(err)
			}
			if p.InsertionTail != "task-a" {
				t.Fatal("whole-task completion did not set cursor")
			}
		})
	}
}

func TestSharedAdvanceProtectsAllConfirmedHistory(t *testing.T) {
	base := planInState(t, Active)
	if _, err := base.CompleteManual("task-a", "active", "", testTime); err != nil {
		t.Fatal(err)
	}
	base.Actions = []Receipt{{ID: "known-receipt", Digest: strings.Repeat("a", 64)}}
	for _, kind := range []string{"completion", "attempt", "author", "receipt", "receipt-digest", "task"} {
		t.Run(kind, func(t *testing.T) {
			next := cloneSharedTest(t, base)
			switch kind {
			case "completion":
				next.Tasks[0].Attempts[0].Completion = nil
				next.Tasks[0].Attempts[0].Status = Active
			case "attempt":
				next.Tasks[0].Attempts = nil
			case "author":
				next.Tasks[0].Attempts[0].Author = "replacement"
			case "receipt":
				next.Actions = nil
			case "receipt-digest":
				next.Actions[0].Digest = strings.Repeat("b", 64)
			case "task":
				next.Tasks = next.Tasks[1:]
			}
			if err := ValidateSharedAdvance(base, next); !errors.Is(err, ErrSharedConflict) {
				t.Fatalf("lost history accepted: %v", err)
			}
		})
	}
	late := cloneSharedTest(t, base)
	if _, err := late.Start("task-a", attempt("late", "late"), true); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSharedAdvance(base, late); err != nil {
		t.Fatalf("new approach mistaken for rollback: %v", err)
	}
}
