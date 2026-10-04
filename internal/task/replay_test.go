package task

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestActionReceiptSurvivesStorageIndentation(t *testing.T) {
	before := planForTest(t)
	after := cloneSharedTest(t, before)
	if _, err := after.Start("task-a", attempt("receipt-attempt", "receipt-work"), false); err != nil {
		t.Fatal(err)
	}
	action := sharedAction(t, before, after, "receipt-action")
	merged, err := Replay(before, []Action{action})
	if err != nil {
		t.Fatal(err)
	}
	digest := action.Digest()
	for _, data := range []*json.RawMessage{&action.Before, &action.After} {
		var buffer bytes.Buffer
		if err := json.Indent(&buffer, *data, "  ", "    "); err != nil {
			t.Fatal(err)
		}
		*data = append([]byte(nil), buffer.Bytes()...)
	}
	if action.Digest() != digest {
		t.Fatal("formatting changed action identity")
	}
	result, err := Replay(merged, []Action{action})
	if err != nil || len(result.Actions) != 1 {
		t.Fatalf("receipt not recognized: %+v %v", result, err)
	}
}

func TestConnectingSameKnownActiveTaskDoesNotInventCollision(t *testing.T) {
	empty, err := NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	local := planInState(t, Active)
	remote := cloneSharedTest(t, local)
	if _, err := MergeShared(empty, local, remote); err != nil {
		t.Fatalf("identical active snapshot rejected: %v", err)
	}
}
