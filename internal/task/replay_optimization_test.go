package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestReplayRepeatedSnapshotsAndReceiptCollisions(t *testing.T) {
	before := planForTest(t)
	after := cloneSharedTest(t, before)
	if _, err := after.Start("task-a", attempt("cached-attempt", "cached-work"), false); err != nil {
		t.Fatal(err)
	}
	action := sharedAction(t, before, after, "cached-action")
	merged, err := Replay(before, []Action{action, action})
	if err != nil || len(merged.Actions) != 1 {
		t.Fatalf("repeated action: %+v %v", merged, err)
	}
	want, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := Replay(merged, []Action{action})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(repeated)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("acknowledged action changed serialized content: %v", err)
	}
	collision := action
	collision.After = action.Before
	if _, err := Replay(before, []Action{action, collision}); !errors.Is(err, ErrSharedConflict) {
		t.Fatalf("same-call collision accepted: %v", err)
	}
	if _, err := Replay(merged, []Action{collision}); !errors.Is(err, ErrSharedConflict) {
		t.Fatalf("receipt collision accepted: %v", err)
	}
}

func TestTeamValidationDoesNotRetainSnapshotCache(t *testing.T) {
	p := planForTest(t)
	data, err := SharedBytes(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Team = &TeamState{Remote: "origin", Base: data, Pending: []Action{{ID: "same", Before: data, After: data}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Team.Pending[0].After = json.RawMessage(`{"format":"git-task","schema_version":3,"tasks":[]}`)
	if err := p.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mutated snapshot accepted: %v", err)
	}
}

func TestObserveNoOpStillValidatesPlan(t *testing.T) {
	p := planForTest(t)
	if _, err := p.Start("task-a", attempt("current", "feature"), false); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"current", ""} {
		if changed, err := p.Observe("task-a", id, nil, nil); err != nil || changed {
			t.Fatalf("no-op %q: %v %v", id, changed, err)
		}
	}
	p.Order[0] = "missing"
	if _, err := p.Observe("task-a", "current", nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid no-op accepted: %v", err)
	}
	if _, err := p.SetWarnings("task-a", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid warnings no-op accepted: %v", err)
	}
}
