package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func legacyPlanBytes(t *testing.T, plan Plan, tail string) []byte {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if tail == "absent" {
		return data
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["insertion_tail"] = json.RawMessage(tail)
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLegacyPlanJSONCompatibility(t *testing.T) {
	for _, status := range []Status{Todo, Active, Done, Archived} {
		for _, tail := range []string{"absent", `""`, `"task-a"`} {
			t.Run(string(status)+"/"+tail, func(t *testing.T) {
				original := planInState(t, status)
				data := legacyPlanBytes(t, original, tail)
				var plan Plan
				if err := json.Unmarshal(data, &plan); err != nil || !reflect.DeepEqual(original, plan) {
					t.Fatalf("legacy read: %+v %v", plan, err)
				}
				applied, changed, err := ApplyPlan(data)
				if err != nil || changed || !reflect.DeepEqual(original, applied) {
					t.Fatalf("legacy apply: changed=%v plan=%+v err=%v", changed, applied, err)
				}
				if _, err := ValidateShared(data); err != nil {
					t.Fatalf("legacy shared read: %v", err)
				}
				added, err := plan.Add("Next", "", Position{})
				if err != nil || plan.Order[len(plan.Order)-1] != added.ID {
					t.Fatalf("legacy add: %v %v", plan.Order, err)
				}
				encoded, err := SharedBytes(plan)
				if err != nil || bytes.Contains(encoded, []byte("insertion_tail")) {
					t.Fatalf("legacy field emitted: %s %v", encoded, err)
				}
			})
		}
	}
}

func TestLegacyPlanJSONRejectsInvalidCursor(t *testing.T) {
	for _, raw := range []string{`null`, `3`, `[]`, `{}`, `"unknown"`, `"\u0001"`} {
		data := legacyPlanBytes(t, planForTest(t), raw)
		var plan Plan
		if err := json.Unmarshal(data, &plan); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid legacy cursor accepted %s: %v", raw, err)
		}
		if _, _, err := ApplyPlan(data); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid legacy cursor applied %s: %v", raw, err)
		}
	}
}

func TestLegacyActionSnapshotsAndReceiptsSurvive(t *testing.T) {
	before := planInState(t, Active)
	after := cloneSharedTest(t, before)
	if _, err := after.CompleteManual("task-a", "active", "", testTime); err != nil {
		t.Fatal(err)
	}
	action := Action{ID: "legacy-action", Before: legacyPlanBytes(t, before, `""`), After: legacyPlanBytes(t, after, `"task-a"`)}
	digest := action.Digest()
	oldBefore, oldAfter := bytes.Clone(action.Before), bytes.Clone(action.After)
	replayed, err := Replay(before, []Action{action})
	if err != nil || len(replayed.Actions) != 1 || replayed.Actions[0] != (Receipt{ID: action.ID, Digest: digest}) {
		t.Fatalf("legacy replay: %+v %v", replayed.Actions, err)
	}
	plan := after
	plan.Team = &TeamState{Remote: "origin", Base: legacyPlanBytes(t, before, `"task-a"`), Pending: []Action{action}}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	queued, err := QueueApplied(plan)
	if err != nil || len(queued.Team.Pending) != 1 || !bytes.Equal(queued.Team.Pending[0].Before, oldBefore) || !bytes.Equal(queued.Team.Pending[0].After, oldAfter) {
		t.Fatalf("no-op changed old snapshots: %v", err)
	}
	if _, err := plan.Add("New task", "", Position{}); err != nil {
		t.Fatal(err)
	}
	queued, err = QueueApplied(plan)
	if err != nil || len(queued.Team.Pending) != 2 || queued.Team.Pending[0].Digest() != digest {
		t.Fatalf("new action lost old digest: %v", err)
	}
	latest := queued.Team.Pending[1]
	if bytes.Contains(latest.Before, []byte("insertion_tail")) || bytes.Contains(latest.After, []byte("insertion_tail")) || !bytes.Equal(queued.Team.Pending[0].Before, oldBefore) || !bytes.Equal(queued.Team.Pending[0].After, oldAfter) {
		t.Fatal("new action emitted cursor or rewrote historical action")
	}
	data, err := json.Marshal(queued)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Plan
	if err := json.Unmarshal(data, &roundtrip); err != nil || roundtrip.Team.Pending[0].Digest() != digest {
		t.Fatalf("round trip changed digest: %v", err)
	}
	result, err := Replay(replayed, roundtrip.Team.Pending)
	if err != nil || len(result.Actions) != 2 || len(result.Tasks) != 3 {
		t.Fatalf("repeat duplicated actions: %+v %v", result, err)
	}
}
