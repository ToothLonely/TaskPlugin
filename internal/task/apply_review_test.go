package task

import (
	"encoding/json"
	"testing"
)

func TestApplyPlanReviewValidEmptyInsertionTail(t *testing.T) {
	for name, input := range map[string]string{
		"empty": `{"format":"git-task","schema_version":3,"revision":0,"last_event":0,"target_branch":"main","tasks":[],"order":[],"insertion_tail":""}`,
		"todo":  `{"format":"git-task","schema_version":3,"revision":0,"last_event":0,"target_branch":"main","tasks":[{"id":"a","title":"A","status":"todo","revision":1}],"order":["a"],"insertion_tail":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			var original Plan
			if err := json.Unmarshal([]byte(input), &original); err != nil {
				t.Fatalf("valid ready plan: %v", err)
			}
			if _, changed, err := ApplyPlan([]byte(input)); err != nil || changed {
				t.Fatalf("unchanged valid plan must be a no-op: changed=%v err=%v", changed, err)
			}
		})
	}
}
