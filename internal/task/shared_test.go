package task

import (
	"encoding/json"
	"errors"
	"testing"
)

func sharedAction(t *testing.T, before, after Plan, id string) Action {
	t.Helper()
	b, err := SharedBytes(before)
	if err != nil {
		t.Fatal(err)
	}
	a, err := SharedBytes(after)
	if err != nil {
		t.Fatal(err)
	}
	return Action{ID: id, Before: b, After: a}
}

func cloneSharedTest(t *testing.T, p Plan) Plan {
	t.Helper()
	data, err := SharedBytes(p)
	if err != nil {
		t.Fatal(err)
	}
	var result Plan
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestIndependentOfflineStartsAndDeliveryRepeats(t *testing.T) {
	base := planForTest(t)
	left, right := cloneSharedTest(t, base), cloneSharedTest(t, base)
	a, b := attempt("left", "left-branch"), attempt("right", "right-branch")
	a.Author, b.Author = "Alice", "Bob"
	if _, err := left.Start("task-a", a, false); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Start("task-a", b, false); err != nil {
		t.Fatal(err)
	}
	la, ra := sharedAction(t, base, left, "action-left"), sharedAction(t, base, right, "action-right")
	for _, actions := range [][]Action{{la, ra, la, ra}, {ra, la, ra, la}} {
		merged, err := Replay(base, actions)
		if err != nil {
			t.Fatal(err)
		}
		item, err := merged.FindID("task-a")
		if err != nil || item.Status != Active || len(item.Attempts) != 2 || len(merged.Actions) != 2 {
			t.Fatalf("merged: %+v %v", merged, err)
		}
		for _, a := range []Attempt{a, b} {
			got, err := item.Attempt(a.ID)
			if err != nil || got.Author != a.Author {
				t.Fatalf("author/ID lost: %+v %v", got, err)
			}
		}
	}
}

func TestCompletionAndLateOfflineStart(t *testing.T) {
	base := planForTest(t)
	late := cloneSharedTest(t, base)
	if _, err := late.Start("task-a", attempt("late", "late-branch"), false); err != nil {
		t.Fatal(err)
	}
	current := cloneSharedTest(t, base)
	if _, err := current.Start("task-a", attempt("known", "known-branch"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := current.CompleteManual("task-a", "known", "", testTime); err != nil {
		t.Fatal(err)
	}
	merged, err := Replay(current, []Action{sharedAction(t, base, late, "late-action")})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := merged.FindID("task-a")
	if item.Status != Active || len(item.Attempts) != 2 {
		t.Fatalf("late start lost: %+v", item)
	}
	if _, err := merged.Pause("task-a", "late"); err != nil {
		t.Fatal(err)
	}
	item, _ = merged.FindID("task-a")
	if item.Status != Paused {
		t.Fatalf("pause: %+v", item)
	}
	if _, err := merged.CompleteManual("task-a", "late", "", testTime); err != nil {
		t.Fatal(err)
	}
	item, _ = merged.FindID("task-a")
	if item.Status != Done || item.Attempts[0].Completion == nil || item.Attempts[1].Completion == nil {
		t.Fatalf("all-done: %+v", item)
	}
}

func TestStaleActiveCannotUndoCompletedAttempt(t *testing.T) {
	base := planInState(t, Active)
	paused, done := cloneSharedTest(t, base), cloneSharedTest(t, base)
	if _, err := paused.Pause("task-a", "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := done.CompleteManual("task-a", "active", "", testTime); err != nil {
		t.Fatal(err)
	}
	merged, err := Replay(done, []Action{sharedAction(t, base, paused, "pause-offline")})
	if err != nil {
		t.Fatal(err)
	}
	item, _ := merged.FindID("task-a")
	if item.Status != Done || item.Attempts[0].Status != Done {
		t.Fatalf("completion reverted: %+v", item)
	}
}

func TestSameIDAndManualFieldConflictsAreRejected(t *testing.T) {
	base := planForTest(t)
	left, right := cloneSharedTest(t, base), cloneSharedTest(t, base)
	lt, rt := "Rename left", "Rename right"
	left.Edit("task-a", EditOptions{Title: &lt})
	right.Edit("task-a", EditOptions{Title: &rt})
	if _, err := MergeShared(base, left, right); !errors.Is(err, ErrSharedConflict) {
		t.Fatalf("rename conflict: %v", err)
	}
	left, right = cloneSharedTest(t, base), cloneSharedTest(t, base)
	left.Start("task-a", attempt("collision", "left"), false)
	right.Start("task-a", attempt("collision", "right"), false)
	if _, err := MergeShared(base, left, right); !errors.Is(err, ErrSharedConflict) {
		t.Fatalf("attempt collision: %v", err)
	}
	a := sharedAction(t, base, left, "same-action")
	merged, err := Replay(base, []Action{a})
	if err != nil {
		t.Fatal(err)
	}
	a.After, _ = SharedBytes(right)
	if _, err := Replay(merged, []Action{a}); !errors.Is(err, ErrSharedConflict) {
		t.Fatalf("action collision: %v", err)
	}
}

func TestMultipleAttemptSelectorsAndAtomicAmbiguity(t *testing.T) {
	p := planForTest(t)
	p.Start("task-a", attempt("one", "one"), false)
	p.Start("task-a", attempt("two", "two"), false)
	before := snapshot(t, p)
	if _, err := p.Pause("task-a"); !errors.Is(err, ErrAmbiguous) || snapshot(t, p) != before {
		t.Fatalf("ambiguous pause: %v", err)
	}
	if _, err := p.Pause("task-a", "one"); err != nil {
		t.Fatal(err)
	}
	item, _ := p.FindID("task-a")
	if item.Status != Active || item.Attempts[0].Status != Paused || item.Attempts[1].Status != Active {
		t.Fatalf("other attempt changed: %+v", item)
	}
	if _, err := p.CompleteManual("task-a", "one", "", testTime); err != nil {
		t.Fatal(err)
	}
	item, _ = p.FindID("task-a")
	if item.Status != Active || item.Attempts[1].Completion != nil {
		t.Fatalf("other attempt completed: %+v", item)
	}
}

func TestNewTaskIDsAreIndependentAndLegacyIDsSurvive(t *testing.T) {
	base := planForTest(t)
	left, right := cloneSharedTest(t, base), cloneSharedTest(t, base)
	a, err := left.Add("New left", "", Position{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := right.Add("New right", "", Position{})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || len(a.ID) != 32 || len(b.ID) != 32 {
		t.Fatalf("unstable IDs: %s %s", a.ID, b.ID)
	}
	merged, err := MergeShared(base, left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Tasks) != 4 {
		t.Fatal("task lost")
	}
	for _, id := range []string{"task-a", "task-b", a.ID, b.ID} {
		if _, err := merged.FindID(id); err != nil {
			t.Fatal(err)
		}
	}
}
