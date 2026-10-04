package task

import (
	"fmt"
	"math"
)

// Format and SchemaVersion identify this plan representation.
const (
	Format        = "git-task"
	SchemaVersion = 2
)

// Plan keeps explicit order separate from stable task identities.
// Callers must serialize access; Revision does not replace a storage lock.
type Plan struct {
	Format        string        `json:"format"`
	SchemaVersion int           `json:"schema_version"`
	Revision      uint64        `json:"revision"`
	TargetBranch  string        `json:"target_branch"`
	Order         []string      `json:"order"`
	Tasks         []Task        `json:"tasks"`
	LastEvent     uint64        `json:"last_event"`
	InsertionTail string        `json:"insertion_tail,omitempty"`
	Team          *TeamState    `json:"team,omitempty"`
	Actions       []Receipt     `json:"actions,omitempty"`
	ServerEvents  []ServerEvent `json:"server_events,omitempty"`
}

// NewPlan returns an empty plan for a literal local target branch.
func NewPlan(target string) (Plan, error) {
	p := Plan{Format: Format, SchemaVersion: SchemaVersion, TargetBranch: target, Order: []string{}, Tasks: []Task{}}
	return p, p.Validate()
}

func (p Plan) index(id string) (int, error) {
	for i := range p.Tasks {
		if p.Tasks[i].ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrNotFound, id)
}

// FindID returns an independent snapshot, using the exact JSON ID.
func (p Plan) FindID(id string) (Task, error) {
	i, err := p.index(id)
	if err != nil {
		return Task{}, err
	}
	return p.Tasks[i].clone(), nil
}

// FindTitle searches all states, including archived, without normalization.
func (p Plan) FindTitle(title string) (Task, error) {
	if blank(title) {
		return Task{}, invalid("пустой title")
	}
	var matches []string
	for _, t := range p.Tasks {
		if t.Title == title {
			matches = append(matches, t.ID)
		}
	}
	if len(matches) == 0 {
		return Task{}, fmt.Errorf("%w: title %q", ErrNotFound, title)
	}
	if len(matches) > 1 {
		return Task{}, fmt.Errorf("%w: %q: %v", ErrAmbiguous, title, matches)
	}
	return p.FindID(matches[0])
}

// FirstTodo uses the explicit plan order, never the tasks slice or display number.
func (p Plan) FirstTodo() (Task, error) {
	if err := p.Validate(); err != nil {
		return Task{}, err
	}
	for _, id := range p.Order {
		t, _ := p.FindID(id)
		if t.Status == Todo {
			return t, nil
		}
	}
	return Task{}, fmt.Errorf("%w: нет не начатых задач", ErrNotFound)
}

// change commits a validated copy only after the whole transition succeeds.
// This is an in-memory guarantee; durable transactions belong to storage/app.
func (p *Plan) change(id string, apply func(*Plan, *Task) (bool, error)) (bool, error) {
	if err := p.Validate(); err != nil {
		return false, err
	}
	i, err := p.index(id)
	if err != nil {
		return false, err
	}
	next := *p
	if p.Team != nil {
		team := *p.Team
		team.Pending = append([]Action(nil), p.Team.Pending...)
		team.LocalAttempts = append([]string(nil), p.Team.LocalAttempts...)
		next.Team = &team
	}
	next.Tasks = append([]Task(nil), p.Tasks...)
	next.Tasks[i] = p.Tasks[i].clone()
	changed, err := apply(&next, &next.Tasks[i])
	if err != nil || !changed {
		return false, err
	}
	if next.Revision == math.MaxUint64 || next.Tasks[i].Revision == math.MaxUint64 {
		return false, invalid("исчерпан счётчик ревизий")
	}
	next.Revision++
	next.Tasks[i].Revision++
	if err := next.Validate(); err != nil {
		return false, err
	}
	*p = next
	return true, nil
}
