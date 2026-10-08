package app

import (
	"errors"
	"strings"
	"testing"

	"git-task/internal/task"
)

func TestTaskIDSelectors(t *testing.T) {
	plan, err := task.NewPlan("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"abcd", "abcd1234", "abef5678", "unique1234", "001", "0019", "UPPER1234"} {
		plan.Tasks = append(plan.Tasks, task.Task{ID: id, Title: id, Status: task.Todo})
		plan.Order = append(plan.Order, id)
	}
	plan.Tasks[2].Status = task.Archived
	cases := []struct {
		selector string
		want     string
		err      error
	}{
		{selector: "abcd", want: "abcd"},
		{selector: "abcd1", want: "abcd1234"},
		{selector: "u", want: "unique1234"},
		{selector: "001", want: "001"},
		{selector: "UP", want: "UPPER1234"},
		{selector: "abe", want: "abef5678"},
		{selector: "ab", err: task.ErrAmbiguous},
		{selector: "00", err: task.ErrAmbiguous},
		{selector: "up", err: task.ErrNotFound},
		{selector: " unique", err: task.ErrNotFound},
		{selector: "missing", err: task.ErrNotFound},
		{selector: "", err: task.ErrNotFound},
		{selector: " ", err: task.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.selector, func(t *testing.T) {
			item, err := selectTaskID(plan, tc.selector)
			if !errors.Is(err, tc.err) || item.ID != tc.want {
				t.Fatalf("selector=%q got=%q err=%v; want=%q err=%v", tc.selector, item.ID, err, tc.want, tc.err)
			}
			if tc.err == nil {
				item.Title = "caller change"
				original, err := plan.FindID(tc.want)
				if err != nil || original.Title != tc.want {
					t.Fatalf("selection changed source: %+v err=%v", original, err)
				}
			}
		})
	}
	if _, err := plan.FindID("abcd1"); !errors.Is(err, task.ErrNotFound) {
		t.Fatalf("internal ID lookup accepts a prefix: %v", err)
	}
	plan.Order[0] = "abcd1"
	if err := plan.Validate(); !errors.Is(err, task.ErrInvalid) {
		t.Fatalf("stored order accepts a prefix: %v", err)
	}
}

func TestTaskIDPrefixAmbiguityListsAllMatchingIDs(t *testing.T) {
	plan := task.Plan{Tasks: []task.Task{
		{ID: "abcd2222", Title: "Archived", Status: task.Archived},
		{ID: "abcd1111", Title: "Todo", Status: task.Todo},
	}}
	_, err := selectTaskID(plan, "abcd")
	if !errors.Is(err, task.ErrAmbiguous) || !strings.Contains(err.Error(), "abcd1111, abcd2222") {
		t.Fatalf("ambiguous selector has no useful candidates: %v", err)
	}
}
