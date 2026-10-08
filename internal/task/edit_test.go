package task

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestEditFieldsInEveryState(t *testing.T) {
	for _, status := range []Status{Todo, Active, Paused, Done, Archived} {
		t.Run(string(status), func(t *testing.T) {
			p := planInState(t, status)
			p.Tasks[0].Description = "original"
			before := p.Tasks[0].clone()
			order := slices.Clone(p.Order)
			tail, event, rev := p.InsertionTail, p.LastEvent, p.Revision
			title, description := "  Новое имя 🐈  ", ""
			changed, err := p.Edit("task-a", EditOptions{Title: &title})
			requireChange(t, changed, err)
			before.Title, before.Revision = title, before.Revision+1
			if !reflect.DeepEqual(p.Tasks[0], before) {
				t.Fatalf("title edit changed other fields: got %+v want %+v", p.Tasks[0], before)
			}
			title = "caller changed text"
			changed, err = p.Edit("task-a", EditOptions{Description: &description})
			requireChange(t, changed, err)
			before.Description, before.Revision = "", before.Revision+1
			if !reflect.DeepEqual(p.Tasks[0], before) || !slices.Equal(p.Order, order) || p.InsertionTail != tail || p.LastEvent != event || p.Revision != rev+2 {
				t.Fatalf("description edit changed unrelated data: %+v", p)
			}
			beforeJSON := snapshot(t, p)
			for _, opts := range []EditOptions{{}, {Title: &before.Title, Description: &description}} {
				changed, err = p.Edit("task-a", opts)
				if changed || err != nil || snapshot(t, p) != beforeJSON {
					t.Fatalf("no-op: changed=%v err=%v", changed, err)
				}
			}
		})
	}
}

func TestEditDuplicateTitlesStillPositionByID(t *testing.T) {
	p := orderedPlan(t, "A", "B", "C")
	title, description := "одинаковые", "Описание\nс новой строкой"
	for _, id := range []string{"A", "C"} {
		changed, err := p.Edit(id, EditOptions{Title: &title, Description: &description})
		requireChange(t, changed, err)
	}
	if _, err := p.FindTitle(title); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("duplicate titles: %v", err)
	}
	before := snapshot(t, p)
	changed, err := p.Move("B", Position{Before: title})
	if changed || !errors.Is(err, ErrNotFound) || snapshot(t, p) != before {
		t.Fatalf("title accepted as position ID: changed=%v err=%v", changed, err)
	}
	changed, err = p.Move("C", Position{Before: "A"})
	requireChange(t, changed, err)
	if !slices.Equal(p.Order, []string{"C", "A", "B"}) {
		t.Fatalf("order=%v", p.Order)
	}
	first, err := p.FirstTodo()
	if err != nil || first.ID != "C" {
		t.Fatalf("first todo=%+v err=%v", first, err)
	}
	added, err := p.Add(title, "", Position{After: "A"})
	if err != nil || !slices.Equal(p.Order, []string{"C", "A", added.ID, "B"}) {
		t.Fatalf("add by ID after rename: order=%v err=%v", p.Order, err)
	}
}

func TestEditErrorsLeavePlanUnchanged(t *testing.T) {
	tests := []struct {
		name, id, title, description string
		prepare                      func(*Plan)
		err                          error
	}{
		{name: "unknown ID", id: "missing", title: "valid", err: ErrNotFound},
		{name: "unknown ID", id: "missing-task", title: "valid", err: ErrNotFound},
		{name: "title is not ID", id: "Профиль", title: "valid", err: ErrNotFound},
		{name: "empty title", id: "task-a", err: ErrInvalid},
		{name: "blank title", id: "task-a", title: "\t\u2003\n", err: ErrInvalid},
		{name: "bad title UTF8", id: "task-a", title: "\xff", err: ErrInvalid},
		{name: "bad description UTF8", id: "task-a", title: "valid", description: "\xff", err: ErrInvalid},
		{name: "plan revision overflow", id: "task-a", title: "valid", prepare: func(p *Plan) { p.Revision = math.MaxUint64 }, err: ErrInvalid},
		{name: "task revision overflow", id: "task-a", title: "valid", prepare: func(p *Plan) { p.Tasks[0].Revision = math.MaxUint64 }, err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := planInState(t, Active)
			if tt.prepare != nil {
				tt.prepare(&p)
			}
			before := snapshot(t, p)
			changed, err := p.Edit(tt.id, EditOptions{Title: &tt.title, Description: &tt.description})
			if changed || !errors.Is(err, tt.err) || snapshot(t, p) != before {
				t.Fatalf("changed=%v err=%v; want %v and unchanged plan", changed, err, tt.err)
			}
		})
	}
}
