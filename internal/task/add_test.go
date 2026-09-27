package task

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestAddIdentityAndText(t *testing.T) {
	p := orderedPlan(t)
	first, err := p.Add("  Профиль 🐈  ", "строка 1\nстрока 2", Position{})
	if err != nil {
		t.Fatal(err)
	}
	want := Task{ID: "task-001", Number: "T-001", Title: "  Профиль 🐈  ", Description: "строка 1\nстрока 2", Status: Todo, Revision: 1}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(p.Tasks, []Task{want}) || p.Revision != 1 {
		t.Fatalf("Add returned %+v, plan=%+v; want %+v", first, p, want)
	}
	first.Title = "caller change"
	if p.Tasks[0].Title != want.Title {
		t.Fatal("returned task aliases the plan")
	}
	changed, err := p.Archive(first.ID)
	requireChange(t, changed, err)
	second, err := p.Add(want.Title, "", Position{Before: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "task-002" || second.Number != "T-002" || p.Order[0] != second.ID || p.InsertionTail != first.ID {
		t.Fatalf("identity depends on position or reuses archived number: %+v", p)
	}
	if _, err := p.FindTitle(want.Title); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate title: %v", err)
	}
	if next, err := p.FirstTodo(); err != nil || next.ID != second.ID {
		t.Fatalf("first todo=%+v, err=%v", next, err)
	}
}

func TestAddImportedIdentities(t *testing.T) {
	tests := []struct {
		name, id, number, wantID, wantNumber string
	}{
		{"opaque", "external-id", "external-number", "task-001", "T-001"},
		{"ID reserves suffix", "task-009", "external-number", "task-010", "T-010"},
		{"number reserves suffix", "external-id", "T-099", "task-100", "T-100"},
		{"different suffixes", "task-120", "T-999", "task-1000", "T-1000"},
		{"leading zeros", "task-00012", "T-00009", "task-013", "T-013"},
		{"zero", "task-000", "T-000", "task-001", "T-001"},
		{"not a decimal suffix", "task-+12", "T-12a", "task-001", "T-001"},
		{"empty suffix", "task-", "T-", "task-001", "T-001"},
		{"last available", "task-18446744073709551614", "T-004", "task-18446744073709551615", "T-18446744073709551615"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := orderedPlan(t, tt.id)
			p.Tasks[0].Number = tt.number
			p.Tasks[0].Status = Archived
			oldTask := p.Tasks[0]
			added, err := p.Add("new", "", Position{})
			if err != nil || added.ID != tt.wantID || added.Number != tt.wantNumber || !reflect.DeepEqual(p.Tasks[0], oldTask) {
				t.Fatalf("added=%+v err=%v; want %s/%s and unchanged imported task", added, err, tt.wantID, tt.wantNumber)
			}
		})
	}
}

func TestAddErrorsLeavePlanUnchanged(t *testing.T) {
	tests := []struct {
		name               string
		title, description string
		prepare            func(*Plan)
	}{
		{name: "empty title"},
		{name: "blank title", title: " \t\n\u2003"},
		{name: "invalid title UTF8", title: "bad\xff"},
		{name: "invalid description UTF8", title: "valid", description: "bad\xff"},
		{name: "revision overflow", title: "valid", prepare: func(p *Plan) { p.Revision = math.MaxUint64 }},
		{name: "number exhaustion", title: "valid", prepare: func(p *Plan) { p.Tasks[0].Number = "T-18446744073709551615" }},
		{name: "ID exhaustion", title: "valid", prepare: func(p *Plan) {
			p.Tasks[0].ID = "task-18446744073709551615"
			p.Order[0] = p.Tasks[0].ID
		}},
		{name: "numeric suffix overflow", title: "valid", prepare: func(p *Plan) { p.Tasks[0].Number = "T-18446744073709551616" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := orderedPlan(t, "A", "B")
			if tt.prepare != nil {
				tt.prepare(&p)
			}
			before := snapshot(t, p)
			added, err := p.Add(tt.title, tt.description, Position{})
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(added, Task{}) || snapshot(t, p) != before {
				t.Fatalf("added=%+v err=%v; want empty result and unchanged plan", added, err)
			}
		})
	}
}

func TestPlanOperationsRejectInvalidPlan(t *testing.T) {
	for _, operation := range []string{"add", "edit", "move"} {
		t.Run(operation, func(t *testing.T) {
			p := orderedPlan(t, "A", "B")
			p.Order[1] = "A"
			before := orderedPlan(t, "A", "B")
			before.Order[1] = "A"
			var err error
			var changed bool
			switch operation {
			case "add":
				_, err = p.Add("new", "", Position{})
			case "edit":
				title := "new"
				changed, err = p.Edit("A", EditOptions{Title: &title})
			case "move":
				changed, err = p.Move("A", Position{End: true})
			}
			if changed || !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(p, before) {
				t.Fatalf("changed=%v err=%v; want ErrInvalid and unchanged plan", changed, err)
			}
		})
	}
}
