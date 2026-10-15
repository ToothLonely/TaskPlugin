package task

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestApplyPlanPreservesHistoryAndOrder(t *testing.T) {
	for _, state := range []Status{Todo, Active, Done, Archived} {
		t.Run(string(state), func(t *testing.T) {
			plan := planInState(t, state)
			original := plan.Tasks[0].clone()
			data, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			var tasks []map[string]json.RawMessage
			if err := json.Unmarshal(fields["tasks"], &tasks); err != nil {
				t.Fatal(err)
			}
			tasks[0]["title"] = json.RawMessage(`"Edited"`)
			tasks[0]["description"] = json.RawMessage(`"Details"`)
			tasks[0], tasks[1] = tasks[1], tasks[0]
			tasks = append(tasks, map[string]json.RawMessage{"title": json.RawMessage(`"New"`)}, map[string]json.RawMessage{"id": json.RawMessage(`"manual"`), "title": json.RawMessage(`"Manual"`)})
			fields["tasks"], _ = json.Marshal(tasks)
			data, _ = json.Marshal(fields)
			applied, changed, err := ApplyPlan(data)
			if err != nil || !changed {
				t.Fatalf("apply: %v %v", changed, err)
			}
			got, err := applied.FindID(original.ID)
			if err != nil {
				t.Fatal(err)
			}
			original.Title, original.Description = "Edited", "Details"
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("history changed: %+v / %+v", got, original)
			}
			if !reflect.DeepEqual(applied.Order[:len(plan.Order)], plan.Order) {
				t.Fatalf("order: %v", applied.Order)
			}
			for _, item := range applied.Tasks[len(plan.Tasks):] {
				if item.ID == "" || item.Status != Todo || item.Revision != 1 || len(item.Attempts) != 0 {
					t.Fatalf("new task: %+v", item)
				}
			}
			data, _ = json.Marshal(applied)
			repeated, changed, err := ApplyPlan(data)
			if err != nil || changed || !reflect.DeepEqual(applied, repeated) {
				t.Fatalf("repeat: %v %v", changed, err)
			}
			added, err := repeated.Add("Later", "", Position{})
			if err != nil || repeated.Order[len(repeated.Order)-1] != added.ID {
				t.Fatalf("add: %v", err)
			}
		})
	}
}

func TestApplyPlanMissingFieldsAndStrictInput(t *testing.T) {
	input := `{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[{"title":"One"},{"id":"fixed","title":"Two"}]}`
	plan, changed, err := ApplyPlan([]byte(input))
	if err != nil || !changed || len(plan.Order) != 2 || plan.Order[1] != "fixed" || plan.Revision != 1 {
		t.Fatalf("defaults: %+v %v", plan, err)
	}
	for _, suffix := range []string{
		`"id":null`, `"id":""`, `"id":4`, `"id":"\u0001"`, `"extra":true`, `"title":"duplicate"`,
		`"status":"active"`, `"status":null`, `"revision":null`, `"revision":2`,
		`"warnings":[{"code":"x","message":"history"}]`, `"attempts":[{}]`, `"attempts":[]`,
		`"description":"\ud800"`,
	} {
		t.Run(suffix, func(t *testing.T) {
			data := []byte(`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[{"title":"New",` + suffix + `}]}`)
			if _, _, err := ApplyPlan(data); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid accepted: %s: %v", data, err)
			}
		})
	}
	for _, input := range []string{
		`{"format":"git-task","schema_version":2,"target_branch":"main","tasks":[]}`,
		`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[],"order":["unknown"]}`,
		`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[{"id":"a","title":"A"},{"id":"a","title":"B"}]}`,
		`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[],"order":null}`,
		`{"format":"git-task","schema_version":3,"target_branch":"main","tasks":[],"insertion_tail":"lost"}`,
		"{\"format\":\"git-task\",\"schema_version\":3,\"target_branch\":\"main\",\"tasks\":[{\"title\":\"\xff\"}]}",
	} {
		if _, _, err := ApplyPlan([]byte(input)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid accepted: %s: %v", input, err)
		}
	}
	var strict Plan
	if err := json.Unmarshal([]byte(input), &strict); err == nil {
		t.Fatal("ordinary decoding accepted incomplete plan")
	}
}

func TestApplyPlanRejectsMissingHistoryIdentity(t *testing.T) {
	plan := planInState(t, Done)
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	var entries []map[string]json.RawMessage
	_ = json.Unmarshal(fields["tasks"], &entries)
	delete(entries[0], "id")
	fields["tasks"], _ = json.Marshal(entries)
	data, _ = json.Marshal(fields)
	if _, _, err := ApplyPlan(data); !errors.Is(err, ErrInvalid) {
		t.Fatalf("history reassigned: %v", err)
	}
}
