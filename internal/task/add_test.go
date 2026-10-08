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
	want := Task{ID: first.ID, Title: "  Профиль 🐈  ", Description: "строка 1\nстрока 2", Status: Todo, Revision: 1}
	if len(first.ID) != 32 || !reflect.DeepEqual(first, want) || !reflect.DeepEqual(p.Tasks, []Task{want}) || p.Revision != 1 {
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
	if len(second.ID) != 32 || second.ID == first.ID || p.Order[0] != second.ID || p.InsertionTail != first.ID {
		t.Fatalf("identity depends on position or reuses archived task ID: %+v", p)
	}
	if _, err := p.FindTitle(want.Title); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate title: %v", err)
	}
	if next, err := p.FirstTodo(); err != nil || next.ID != second.ID {
		t.Fatalf("first todo=%+v, err=%v", next, err)
	}
}

func TestAddImportedIdentities(t *testing.T) {
	for _, id := range []string{"001", "task-009", "external-id", "task-18446744073709551616", "0123456789abcdef0123456789abcdef"} {
		t.Run(id, func(t *testing.T) {
			p := orderedPlan(t, id)
			p.Tasks[0].Status = Archived
			original := p.Tasks[0]
			added, err := p.Add("new", "", Position{})
			if err != nil || len(added.ID) != 32 || added.ID == id || !reflect.DeepEqual(p.Tasks[0], original) {
				t.Fatalf("added=%+v err=%v; imported task changed: %+v", added, err, p.Tasks[0])
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
