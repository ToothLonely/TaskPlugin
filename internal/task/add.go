package task

import (
	"math"
	"slices"
)

// Add creates a todo task with a new stable ID, returning its snapshot.
// The default position is the end of the plan. Errors leave p unchanged.
func (p *Plan) Add(title, description string, pos Position) (Task, error) {
	if err := p.Validate(); err != nil {
		return Task{}, err
	}
	i, err := pos.index(p.Order)
	if err != nil {
		return Task{}, err
	}
	id, err := NewID()
	if err != nil {
		return Task{}, err
	}
	t := Task{
		ID:    id,
		Title: title, Description: description, Status: Todo, Revision: 1,
	}
	if p.Revision == math.MaxUint64 {
		return Task{}, invalid("исчерпан счётчик ревизий")
	}
	next := *p
	next.Tasks = append(slices.Clone(p.Tasks), t)
	next.Order = slices.Insert(slices.Clone(p.Order), i, t.ID)
	next.Revision++
	if err := next.Validate(); err != nil {
		return Task{}, err
	}
	*p = next
	return t, nil
}
