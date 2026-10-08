package task

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestTemplateRequiredFieldsAndStrictJSON(t *testing.T) {
	for _, input := range []string{
		`{}`, `{"tasks":[]}`, `{"target_branch":"main"}`,
		`{"target_branch":"main","tasks":null}`,
		`{"target_branch":null,"tasks":[]}`,
		`{"target_branch":"main","tasks":[{}]}`,
		`{"target_branch":"main","tasks":[{"title":" "}]}`,
		`{"target_branch":"main","tasks":[{"title":null}]}`,
		`{"target_branch":"main","tasks":[{"title":"a","id":""}]}`,
		`{"target_branch":"main","tasks":[{"title":"a","id":3}]}`,
		`{"target_branch":"main","tasks":[{"title":"a","id":"x"},{"title":"b","id":"x"}]}`,
		`{"target_branch":"main","tasks":[{"title":"a","status":"done"}]}`,
		`{"target_branch":"main","tasks":[],"order":[]}`,
		`{"target_branch":"main","tasks":[],"format":"other"}`,
		`{"target_branch":"main","target_branch":"other","tasks":[]}`,
		`{"target_branch":"main","tasks":[{"title":"a","title":"b"}]}`,
		`{"Target_branch":"main","tasks":[]}`,
		`{"target_branch":"--bad","tasks":[]}`,
		`{"target_branch":"main","tasks":[{"title":"\ud800"}]}`,
		`{"target_branch":"main","tasks":[]} {}`,
		"{\"target_branch\":\"main\",\"tasks\":[{\"title\":\"\xff\"}]}",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ReadTemplate([]byte(input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted invalid template %q: %v", input, err)
			}
		})
	}
	template, err := ReadTemplate([]byte(`{"target_branch":"","tasks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := template.Plan(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty target applied: %v", err)
	}
}

func TestTemplateGeneratesPlanInArrayOrder(t *testing.T) {
	template, err := ReadTemplate([]byte(`{"target_branch":"main","tasks":[{"title":"First"},{"title":"Second","id":"001","description":"Details"},{"title":"First"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := template.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Format != Format || plan.SchemaVersion != SchemaVersion || plan.TargetBranch != "main" || plan.Revision != 0 || plan.LastEvent != 0 || len(plan.Tasks) != 3 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	for _, index := range []int{0, 2} {
		id := plan.Tasks[index].ID
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 || plan.Tasks[index].Status != Todo || len(plan.Tasks[index].Attempts) != 0 {
			t.Fatalf("unexpected generated task: %+v err=%v", plan.Tasks[index], err)
		}
	}
	if plan.Tasks[1].ID != "001" || plan.Tasks[1].Title != "Second" || plan.Tasks[1].Description != "Details" || plan.Tasks[0].ID == plan.Tasks[2].ID {
		t.Fatalf("identity or text changed: %+v", plan.Tasks)
	}
	if !reflect.DeepEqual(plan.Order, []string{plan.Tasks[0].ID, "001", plan.Tasks[2].ID}) || plan.InsertionTail != plan.Tasks[2].ID {
		t.Fatalf("order differs from tasks: %v", plan.Order)
	}
	added, err := plan.Add("Later", "", Position{})
	if err != nil || plan.Order[len(plan.Order)-1] != added.ID {
		t.Fatalf("new task did not follow template tasks: %+v %v", plan.Order, err)
	}
	empty, err := ReadTemplate([]byte(`{"target_branch":"main","tasks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	emptyPlan, err := empty.Plan()
	if err != nil || emptyPlan.Tasks == nil || emptyPlan.Order == nil || len(emptyPlan.Tasks) != 0 {
		t.Fatalf("empty ready plan: %+v %v", emptyPlan, err)
	}
}
