package task

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func identity(s string) bool {
	return !blank(s) && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// validBranch is a pure structural check, not a substitute for the Git adapter's
// check-ref-format or checking that the branch exists in the repository.
func validBranch(s string) bool {
	if !identity(s) || s == "HEAD" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "refs/") ||
		strings.ContainsAny(s, " ~^:?*[\\") || strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func validOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	if strings.Trim(s, "0") == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validTime(t *time.Time) bool {
	if t == nil {
		return true
	}
	_, offset := t.Zone()
	// RFC3339 has minute precision for offsets. Reject seconds rather than
	// silently losing them on serialization, including historical local zones.
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 &&
		offset > -24*60*60 && offset < 24*60*60 && offset%60 == 0
}

// validJSONTime checks the original offset before time.Parse can normalize
// out-of-range hours/minutes. Validate also checks dates constructed via Go APIs.
func validJSONTime(value string) bool {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || !validTime(&t) {
		return false
	}
	if strings.HasSuffix(value, "Z") {
		return true
	}
	// Successful parsing guarantees the numeric zone suffix has this shape.
	zone := value[len(value)-6:]
	return zone[1:3] < "24" && zone[4:6] < "60"
}

// Validate checks the complete snapshot without repairing or normalizing it.
func (p Plan) Validate() error {
	if p.Format != Format || p.SchemaVersion != SchemaVersion {
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
		if err := t.validate(); err != nil {
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
		if t.Status == InProgress || t.Status == Paused {
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

func (t Task) validate() error {
	switch t.Status {
	case Todo:
		if t.ActiveAttempt != nil || len(t.Attempts) != 0 {
			return invalid("todo не имеет подходов")
		}
	case InProgress, Paused:
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

func (a Attempt) validate() error {
	if !identity(a.ID) || !validTime(a.StartedAt) {
		return invalid("ID или дата подхода")
	}
	bound := a.Branch != ""
	if bound {
		if !validBranch(a.Branch) || !validBranch(a.OriginalBranch) || !validBranch(a.TargetBranch) || a.Branch == a.TargetBranch || a.OriginalBranch == a.TargetBranch || !validOID(a.BaseCommit) || a.StartedAt == nil {
			return invalid("неполная привязка подхода %q", a.ID)
		}
	} else if a.Completion == nil || a.OriginalBranch != "" || a.TargetBranch != "" || a.BaseCommit != "" || a.StartedAt != nil || len(a.Rebindings) > 0 {
		return invalid("неполная привязка подхода %q", a.ID)
	}
	branch := a.OriginalBranch
	for _, r := range a.Rebindings {
		if r.From != branch || !validBranch(r.To) || r.From == r.To || r.To == a.TargetBranch || !validOID(r.BaseCommit) || r.ObservedAt.IsZero() || !validTime(&r.ObservedAt) {
			return invalid("некорректная история перепривязок")
		}
		branch = r.To
	}
	if branch != a.Branch {
		return invalid("история перепривязок не соответствует текущей ветке")
	}
	if len(a.Rebindings) > 0 && a.Rebindings[len(a.Rebindings)-1].BaseCommit != a.BaseCommit {
		return invalid("основание не соответствует последней перепривязке")
	}
	if a.Completion == nil {
		return nil
	}
	c := a.Completion
	if c.Event == 0 || !validBranch(c.TargetBranch) || !validTime(c.CompletedAt) || !validTime(c.ObservedAt) {
		return invalid("событие, цель или дата завершения")
	}
	if bound && c.TargetBranch != a.TargetBranch {
		return invalid("цель завершения отличается от цели подхода")
	}
	if c.Commit != "" && !validOID(c.Commit) {
		return invalid("недопустимый commit завершения")
	}
	switch c.Source {
	case Merge:
		if !bound || c.ObservedAt == nil || !validOID(c.Commit) || !validOID(c.WorkCommit) || (c.MergeKind != MergeCommit && c.MergeKind != FastForward) {
			return invalid("неполное merge-доказательство")
		}
	case Manual, Imported:
		if c.MergeKind != "" || c.WorkCommit != "" {
			return invalid("Git-доказательство у manual/imported")
		}
		if c.Source == Imported && (bound || c.Commit != "") {
			return invalid("выдуманная Git-привязка imported")
		}
	default:
		return invalid("неизвестное основание завершения %q", c.Source)
	}
	return nil
}
