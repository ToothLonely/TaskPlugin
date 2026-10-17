package task

import (
	"slices"

	"git-task/internal/diagnostic"
)

// Position selects at most one explicit location by exact task ID.
// Its zero value selects the end for Add and is invalid for Move.
type Position struct {
	After  string
	Before string
	End    bool
}

func (pos Position) automatic() bool {
	return pos == (Position{})
}

func (pos Position) index(order []string) (int, error) {
	count := 0
	if pos.After != "" {
		count++
	}
	if pos.Before != "" {
		count++
	}
	if pos.End {
		count++
	}
	if count > 1 {
		return 0, invalid("after, before и end взаимоисключающие")
	}
	if pos.End || pos.automatic() {
		return len(order), nil
	}
	anchor := pos.After
	if pos.Before != "" {
		anchor = pos.Before
	}
	i := slices.Index(order, anchor)
	if i < 0 {
		return 0, diagnostic.Errorf("%w: опорная задача %q", ErrNotFound, anchor)
	}
	if pos.Before != "" {
		return i, nil
	}
	return i + 1, nil
}

// Move changes only the explicit order, preserving identities and history.
// Moving to the current position is a no-op; referencing oneself is an error.
func (p *Plan) Move(id string, pos Position) (bool, error) {
	return p.change(id, func(next *Plan, _ *Task) (bool, error) {
		if pos.automatic() {
			return false, invalid("move требует after, before или end")
		}
		if pos.After == id || pos.Before == id {
			return false, invalid("нельзя переместить задачу относительно неё самой")
		}
		// Work on an independent slice: failed validation must not alter p.Order.
		order := slices.Clone(next.Order)
		from := slices.Index(order, id)
		order = slices.Delete(order, from, from+1)
		to, err := pos.index(order)
		if err != nil {
			return false, err
		}
		order = slices.Insert(order, to, id)
		if slices.Equal(order, next.Order) {
			return false, nil
		}
		next.Order = order
		return true, nil
	})
}
