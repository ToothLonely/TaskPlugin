package task

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzPlanJSONAtomic(f *testing.F) {
	f.Add([]byte(`{"format":"git-task","schema_version":2,"revision":0,"target_branch":"main","order":[],"tasks":[],"last_event":0}`))
	f.Add([]byte(`{"format":"git-task","format":"other"}`))
	f.Add([]byte(`{"title":"\ud800"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := NewPlan("main")
		if err != nil {
			t.Fatal(err)
		}
		before, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &p); err != nil {
			after, err := json.Marshal(p)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed decode modified receiver")
			}
			return
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("accepted invalid plan: %v", err)
		}
		var again Plan
		if err := json.Unmarshal(encoded, &again); err != nil {
			t.Fatalf("canonical round trip: %v", err)
		}
	})
}

func FuzzQueuedAction(f *testing.F) {
	base, err := NewPlan("main")
	if err != nil {
		f.Fatal(err)
	}
	data, err := SharedBytes(base)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data, data)
	f.Add(data, []byte(`{"format":"git-task","schema_version":2,"revision":1,"target_branch":"main","order":["fuzz-task"],"tasks":[{"id":"fuzz-task","number":"T-001","title":"Задача 🚀","revision":1,"status":"todo"}],"last_event":0,"insertion_tail":"fuzz-task"}`))
	f.Add([]byte(`{"schema_version":99}`), []byte(`null`))
	f.Fuzz(func(t *testing.T, before, after []byte) {
		original, err := SharedBytes(base)
		if err != nil {
			t.Fatal(err)
		}
		action := Action{ID: "fuzz-action", Before: before, After: after}
		next, replayErr := Replay(base, []Action{action})
		unchanged, err := SharedBytes(base)
		if err != nil || !bytes.Equal(original, unchanged) {
			t.Fatal("replay changed input plan")
		}
		if replayErr != nil {
			return
		}
		if err := next.Validate(); err != nil {
			t.Fatalf("invalid replay result: %v", err)
		}
		once, err := SharedBytes(next)
		if err != nil {
			t.Fatal(err)
		}
		twice, err := Replay(next, []Action{action})
		if err != nil {
			t.Fatalf("receipt replay failed: %v", err)
		}
		encoded, err := SharedBytes(twice)
		if err != nil || !bytes.Equal(once, encoded) {
			t.Fatal("receipt replay duplicated or changed action")
		}
	})
}
