package task

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Add creates a todo task with a new stable ID and number, returning its snapshot.
// Only automatic placement advances InsertionTail. Errors leave p unchanged.
func (p *Plan) Add(title, description string, pos Position) (Task, error) {
	if err := p.Validate(); err != nil {
		return Task{}, err
	}
	i, err := pos.index(p.Order, p.InsertionTail)
	if err != nil {
		return Task{}, err
	}
	n, err := p.nextNumber()
	if err != nil {
		return Task{}, err
	}
	id, err := NewID()
	if err != nil {
		return Task{}, err
	}
	t := Task{
		ID: id, Number: fmt.Sprintf("T-%03d", n),
		Title: title, Description: description, Status: Todo, Revision: 1,
	}
	if p.Revision == math.MaxUint64 {
		return Task{}, invalid("исчерпан счётчик ревизий")
	}
	next := *p
	next.Tasks = append(slices.Clone(p.Tasks), t)
	next.Order = slices.Insert(slices.Clone(p.Order), i, t.ID)
	if pos.automatic() {
		next.InsertionTail = t.ID
	}
	next.Revision++
	if err := next.Validate(); err != nil {
		return Task{}, err
	}
	*p = next
	return t, nil
}

// Retained tasks (including archived ones) are the counter: the model has no
// physical deletion. Imported identities outside these numeric forms stay opaque.
func (p Plan) nextNumber() (uint64, error) {
	var last uint64
	for _, t := range p.Tasks {
		for _, field := range []struct{ value, prefix string }{{t.ID, "task-"}, {t.Number, "T-"}} {
			suffix, ok := strings.CutPrefix(field.value, field.prefix)
			if !ok || suffix == "" || strings.Trim(suffix, "0123456789") != "" {
				continue
			}
			n, err := strconv.ParseUint(suffix, 10, 64)
			if err != nil {
				return 0, invalid("числовой идентификатор %q превышает uint64", field.value)
			}
			last = max(last, n)
		}
	}
	if last == math.MaxUint64 {
		return 0, invalid("исчерпан счётчик номеров задач")
	}
	return last + 1, nil
}
