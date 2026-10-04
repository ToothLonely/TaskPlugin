package task

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// Start applies an already successful Git start to an explicitly resolved ID.
// The application checks selector/--again syntax and Git facts before calling it.
func (p *Plan) Start(id string, a Attempt, again bool) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status == Done && !again {
			return false, ErrAgainRequired
		}
		if t.Status == Archived || again && t.Status != Done {
			return false, fmt.Errorf("%w: start из %s; для paused используйте resume", ErrTransition, t.Status)
		}
		if a.Completion != nil || len(a.Rebindings) != 0 || a.OriginalBranch != a.Branch {
			return false, invalid("start требует новый подход")
		}
		copy := a.clone()
		copy.Status = Active
		t.Attempts = append(t.Attempts, copy)
		t.Status = t.AggregateStatus()
		t.project(copy.ID)
		return true, nil
	})
}

// Attach starts a todo task with an existing branch, without any checkout here.
func (p *Plan) Attach(id string, a Attempt) (bool, error) {
	t, err := p.FindID(id)
	if err != nil {
		return false, err
	}
	if t.Status == Archived || t.Status == Done {
		return false, fmt.Errorf("%w: attach из %s", ErrTransition, t.Status)
	}
	return p.Start(id, a, false)
}

// Pause preserves the active attempt and is a no-op when already paused.
func (p *Plan) Pause(id string, attemptIDs ...string) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status == Archived {
			return false, ErrTransition
		}
		a, err := t.selected(attemptIDs)
		if err != nil {
			return false, err
		}
		if a.Status == Paused {
			return false, nil
		}
		a.Status = Paused
		t.Status = t.AggregateStatus()
		return true, nil
	})
}

// Resume applies a successful switch to the paused task's branch.
func (p *Plan) Resume(id string, attemptIDs ...string) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status == Archived {
			return false, ErrTransition
		}
		a, err := t.selected(attemptIDs)
		if err != nil {
			return false, err
		}
		if a.Status != Paused {
			return false, ErrTransition
		}
		a.Status = Active
		t.Status = t.AggregateStatus()
		return true, nil
	})
}

// Archive freezes the active snapshot and releases its live branch binding.
func (p *Plan) Archive(id string) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status == Archived {
			return false, nil
		}
		t.Status = Archived
		return true, nil
	})
}

// Rebind records an explicit replacement binding. The caller supplies fresh Git
// observations; previous tracking evidence must not be reused for this binding.
func (p *Plan) Rebind(id, branch, base string, observed time.Time, attemptIDs ...string) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if t.Status != Active && t.Status != Paused {
			return false, fmt.Errorf("%w: rebind из %s", ErrTransition, t.Status)
		}
		a, err := t.selected(attemptIDs)
		if err != nil {
			return false, err
		}
		a.Rebindings = append(a.Rebindings, Rebinding{From: a.Branch, To: branch, BaseCommit: base, ObservedAt: observed})
		a.Branch, a.BaseCommit = branch, base
		a.Observation = nil
		t.clearAttemptWarnings(a.ID)
		return true, nil
	})
}

// Complete registers a completion for attemptID. For todo it must be a fresh ID;
// otherwise it must identify the active attempt. A matching historical event is
// a no-op even after a later Start, so delayed sync cannot finish the new attempt.
// Event must be zero: registration order is assigned by this plan, not clocks.
func (p *Plan) Complete(id, attemptID string, c Completion) (bool, error) {
	return p.change(id, func(next *Plan, t *Task) (bool, error) {
		if c.Event != 0 {
			return false, invalid("номер события назначается планом")
		}
		for _, old := range t.Attempts {
			if old.ID == attemptID && old.Completion != nil {
				previous := old.Completion
				if previous.Source != c.Source || previous.Commit != c.Commit || previous.WorkCommit != c.WorkCommit || previous.MergeKind != c.MergeKind || previous.TargetBranch != c.TargetBranch || previous.TargetBefore != c.TargetBefore {
					return false, fmt.Errorf("%w: история завершения неизменяема", ErrTransition)
				}
				return false, nil
			}
		}
		if t.Status != Todo && t.Status != Active && t.Status != Paused {
			return false, fmt.Errorf("%w: complete из %s", ErrTransition, t.Status)
		}
		if c.Source == Imported && t.Status != Todo {
			return false, fmt.Errorf("%w: imported требует todo", ErrTransition)
		}
		if c.TargetBranch != next.TargetBranch {
			return false, invalid("неверная цель завершения")
		}
		a := Attempt{ID: attemptID, Status: Done}
		index := -1
		for i := range t.Attempts {
			if t.Attempts[i].ID == attemptID {
				a = t.Attempts[i].clone()
				index = i
				break
			}
		}
		if index < 0 && t.Status != Todo {
			return false, invalid("завершение другого подхода")
		}
		if next.LastEvent == math.MaxUint64 {
			return false, invalid("исчерпан счётчик событий")
		}
		next.LastEvent++
		c.Event = next.LastEvent
		c.CompletedAt, c.ObservedAt = cloneTime(c.CompletedAt), cloneTime(c.ObservedAt)
		a.Completion = &c
		a.Status = Done
		if index < 0 {
			t.Attempts = append(t.Attempts, a)
		} else {
			t.Attempts[index] = a
		}
		t.Status = t.AggregateStatus()
		t.clearAttemptWarnings(a.ID)
		t.project("")
		if t.Status == Done {
			next.InsertionTail = t.ID
		}
		return true, nil
	})
}

// CompleteManual implements the explicit complete command's no-op rule. A done
// task ignores a repeated command without commit; supplying evidence is an error.
// attemptID is generated by the caller only when completing a todo task.
func (p *Plan) CompleteManual(id, attemptID, commit string, at time.Time) (bool, error) {
	if err := p.Validate(); err != nil {
		return false, err
	}
	t, err := p.FindID(id)
	if err != nil {
		return false, err
	}
	if t.Status == Done {
		if commit == "" {
			return false, nil
		}
		return false, fmt.Errorf("%w: новое доказательство для done", ErrTransition)
	}
	if t.Status != Todo {
		ids := []string{}
		if attemptID != "" {
			ids = append(ids, attemptID)
		}
		a, err := t.selected(ids)
		if err != nil {
			return false, err
		}
		attemptID = a.ID
	}
	return p.Complete(id, attemptID, Completion{Source: Manual, TargetBranch: p.TargetBranch, Commit: commit, CompletedAt: &at, ObservedAt: &at})
}

// SetWarnings replaces diagnostics without inferring a task's state.
func (p *Plan) SetWarnings(id string, warnings []Warning) (bool, error) {
	return p.change(id, func(_ *Plan, t *Task) (bool, error) {
		if slices.Equal(t.Warnings, warnings) {
			return false, nil
		}
		t.Warnings = append([]Warning(nil), warnings...)
		return true, nil
	})
}
