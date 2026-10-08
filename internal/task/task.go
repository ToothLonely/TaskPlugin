// Package task defines the plan and its pure state transitions. It performs no I/O.
package task

import (
	"errors"
	"fmt"
)

// Status is a task's current lifecycle state.
type Status string

const (
	Todo     Status = "todo"
	Active   Status = "active"
	Paused   Status = "paused"
	Done     Status = "done"
	Archived Status = "archived"
)

// Errors can be inspected with errors.Is by application and CLI callers.
var (
	ErrInvalid       = errors.New("некорректные данные плана")
	ErrTransition    = errors.New("недопустимый переход задачи")
	ErrNotFound      = errors.New("задача не найдена")
	ErrAmbiguous     = errors.New("неоднозначный выбор; используйте --id для задачи или --attempt для подхода")
	ErrBranchInUse   = errors.New("ветка уже связана с активной задачей")
	ErrAgainRequired = errors.New("задача выполнена; для нового подхода добавьте --again")
)

// Task has a permanent ID, independent of its title or position.
type Task struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description,omitempty"`
	Revision      uint64    `json:"revision"`
	Status        Status    `json:"status"`
	ActiveAttempt *Attempt  `json:"-"`
	Attempts      []Attempt `json:"attempts,omitempty"`
	Warnings      []Warning `json:"warnings,omitempty"`
}

// Warning records uncertainty without changing lifecycle state.
type Warning struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	AttemptID string `json:"attempt_id,omitempty"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func (t Task) clone() Task {
	t.Attempts = append([]Attempt(nil), t.Attempts...)
	for i := range t.Attempts {
		t.Attempts[i] = t.Attempts[i].clone()
	}
	if t.ActiveAttempt != nil {
		a := t.ActiveAttempt.clone()
		t.ActiveAttempt = &a
	}
	t.Warnings = append([]Warning(nil), t.Warnings...)
	id := ""
	if t.ActiveAttempt != nil {
		id = t.ActiveAttempt.ID
	}
	t.project(id)
	return t
}
