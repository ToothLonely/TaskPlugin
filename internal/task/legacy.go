package task

import (
	"fmt"
	"unicode/utf8"
)

func (p Plan) validateLegacy() error {
	if p.Format != Format || p.SchemaVersion != 1 {
		return invalid("неизвестный формат или версия схемы")
	}
	if !validBranch(p.TargetBranch) {
		return invalid("недопустимая target_branch")
	}
	if p.Tasks == nil || p.Order == nil {
		return invalid("tasks и order должны быть массивами")
	}
	ids, numbers, attempts := map[string]bool{}, map[string]bool{}, map[string]bool{}
	branches := map[string]string{}
	events := map[uint64]bool{}
	var last uint64
	for _, t := range p.Tasks {
		if !identity(t.ID) || !identity(t.Number) || blank(t.Title) || !utf8.ValidString(t.Title) || !utf8.ValidString(t.Description) {
			return invalid("ID, number или title задачи %q", t.ID)
		}
		if ids[t.ID] || numbers[t.Number] {
			return invalid("повтор ID или number задачи %q", t.ID)
		}
		ids[t.ID], numbers[t.Number] = true, true
		if err := t.validateLegacy(); err != nil {
			return fmt.Errorf("задача %q: %w", t.ID, err)
		}
		all := append([]Attempt(nil), t.Attempts...)
		if t.ActiveAttempt != nil {
			all = append(all, *t.ActiveAttempt)
		}
		var previous uint64
		for _, a := range all {
			if attempts[a.ID] {
				return invalid("повтор ID подхода %q", a.ID)
			}
			attempts[a.ID] = true
			if a.Completion != nil {
				e := a.Completion.Event
				if events[e] || e <= previous {
					return invalid("повтор или нарушение порядка событий")
				}
				events[e], previous = true, e
				if e > last {
					last = e
				}
			}
		}
		if t.Status == Active || t.Status == Paused {
			a := t.ActiveAttempt
			if a.TargetBranch != p.TargetBranch {
				return invalid("цель активного подхода отличается от target_branch")
			}
			if owner, ok := branches[a.Branch]; ok {
				return fmt.Errorf("%w: %q (%s, %s)", ErrBranchInUse, a.Branch, owner, t.ID)
			}
			branches[a.Branch] = t.ID
		}
	}
	if p.LastEvent != last {
		return invalid("last_event не соответствует истории")
	}
	if p.InsertionTail != "" && !ids[p.InsertionTail] {
		return invalid("неизвестный insertion_tail")
	}
	if last > 0 && p.InsertionTail == "" {
		return invalid("нет insertion_tail после завершения")
	}
	if len(p.Order) != len(ids) {
		return invalid("order не содержит все задачи")
	}
	seen := map[string]bool{}
	for _, id := range p.Order {
		if !ids[id] || seen[id] {
			return invalid("неизвестный или повторный ID в order: %q", id)
		}
		seen[id] = true
	}
	return nil
}

func (t Task) validateLegacy() error {
	switch t.Status {
	case Todo:
		if t.ActiveAttempt != nil || len(t.Attempts) != 0 {
			return invalid("todo не имеет подходов")
		}
	case Active, Paused:
		if t.ActiveAttempt == nil {
			return invalid("нет текущего подхода")
		}
	case Done:
		if t.ActiveAttempt != nil || len(t.Attempts) == 0 {
			return invalid("done требует историю без active_attempt")
		}
	case Archived:
	default:
		return invalid("неизвестный status %q", t.Status)
	}
	if t.ActiveAttempt != nil {
		if t.ActiveAttempt.Completion != nil {
			return invalid("active_attempt уже завершён")
		}
		if err := t.ActiveAttempt.validate(); err != nil {
			return err
		}
	}
	for _, a := range t.Attempts {
		if a.Completion == nil {
			return invalid("незавершённый подход в attempts")
		}
		if err := a.validate(); err != nil {
			return err
		}
	}
	for _, w := range t.Warnings {
		if !identity(w.Code) || blank(w.Message) || !utf8.ValidString(w.Message) {
			return invalid("пустое предупреждение")
		}
	}
	return nil
}
