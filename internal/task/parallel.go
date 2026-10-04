package task

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

type TeamState struct {
	Remote        string          `json:"remote"`
	Base          json.RawMessage `json:"base"`
	BaseCommit    string          `json:"base_commit,omitempty"`
	Pending       []Action        `json:"pending,omitempty"`
	LocalAttempts []string        `json:"local_attempts,omitempty"`
}

type Action struct {
	ID     string          `json:"id"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

type ServerEvent struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (t Task) AggregateStatus() Status {
	if t.Status == Archived {
		return Archived
	}
	if len(t.Attempts) == 0 {
		return Todo
	}
	status := Done
	for _, a := range t.Attempts {
		if a.Status == Active {
			return Active
		}
		if a.Status == Paused {
			status = Paused
		}
	}
	return status
}

func (t Task) Attempt(id string) (Attempt, error) {
	for _, a := range t.Attempts {
		if a.ID == id {
			return a.clone(), nil
		}
	}
	return Attempt{}, fmt.Errorf("%w: подход %q", ErrNotFound, id)
}

func (t *Task) project(id string) {
	t.ActiveAttempt = nil
	for i := range t.Attempts {
		a := &t.Attempts[i]
		if a.Status == Done {
			continue
		}
		if id != "" && a.ID == id {
			t.ActiveAttempt = a
			return
		}
		if id == "" {
			if t.ActiveAttempt != nil {
				t.ActiveAttempt = nil
				return
			}
			t.ActiveAttempt = a
		}
	}
}

func (t *Task) selected(ids []string) (*Attempt, error) {
	id := ""
	if len(ids) > 1 {
		return nil, invalid("несколько селекторов подхода")
	}
	if len(ids) == 1 {
		id = ids[0]
	}
	if id != "" {
		for i := range t.Attempts {
			if t.Attempts[i].ID != id {
				continue
			}
			if t.Attempts[i].Status == Done {
				return nil, ErrTransition
			}
			t.ActiveAttempt = &t.Attempts[i]
			return t.ActiveAttempt, nil
		}
		return nil, fmt.Errorf("%w: подход %q", ErrNotFound, id)
	}
	if len(t.Attempts) == 0 {
		return nil, ErrTransition
	}
	t.project(id)
	if t.ActiveAttempt == nil {
		return nil, fmt.Errorf("%w: используйте --attempt <id>", ErrAmbiguous)
	}
	return t.ActiveAttempt, nil
}

func (t *Task) clearAttemptWarnings(id string) {
	live := 0
	for _, a := range t.Attempts {
		if a.Status != Done {
			live++
		}
	}
	warnings := make([]Warning, 0, len(t.Warnings))
	for _, w := range t.Warnings {
		if w.AttemptID != id && !(w.AttemptID == "" && live <= 1) {
			warnings = append(warnings, w)
		}
	}
	if len(warnings) == 0 {
		warnings = nil
	}
	t.Warnings = warnings
}

func (t Task) OrderedAttempts() []Attempt {
	result := slices.Clone(t.Attempts)
	slices.SortFunc(result, func(a, b Attempt) int {
		if a.StartedAt != nil && b.StartedAt != nil {
			if c := a.StartedAt.Compare(*b.StartedAt); c != 0 {
				return c
			}
		}
		if a.StartedAt == nil && b.StartedAt != nil {
			return -1
		}
		if a.StartedAt != nil && b.StartedAt == nil {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result
}
