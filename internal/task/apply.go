package task

import (
	"bytes"
	"cmp"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"unicode/utf8"

	"git-task/internal/diagnostic"
)

func ApplyPlan(data []byte) (Plan, bool, error) {
	if !utf8.Valid(data) {
		return Plan{}, false, invalid("JSON не является UTF-8")
	}
	if err := checkUnicodeEscapes(data); err != nil {
		return Plan{}, false, err
	}
	if err := checkJSONFields(json.NewDecoder(bytes.NewReader(data)), true); err != nil {
		return Plan{}, false, diagnostic.Errorf("%w: JSON: %v", ErrInvalid, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return Plan{}, false, invalid("план должен быть объектом")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(fields["tasks"], &entries); err != nil || entries == nil {
		return Plan{}, false, invalid("tasks должен быть массивом объектов")
	}
	changed := false
	fill := func(fields map[string]json.RawMessage, key string, value any) {
		if _, exists := fields[key]; !exists {
			fields[key], _ = json.Marshal(value)
			changed = true
		}
	}
	ids := map[string]bool{}
	for _, entry := range entries {
		if raw, exists := entry["id"]; exists {
			var id string
			if json.Unmarshal(raw, &id) != nil || !identity(id) || ids[id] {
				return Plan{}, false, invalid("недопустимый или повторный ID задачи")
			}
			ids[id] = true
		}
	}
	for _, entry := range entries {
		if entry == nil {
			return Plan{}, false, invalid("задача должна быть объектом")
		}
		_, hasID := entry["id"]
		_, attempts := entry["attempts"]
		_, warnings := entry["warnings"]
		var status Status
		if raw, exists := entry["status"]; exists {
			if json.Unmarshal(raw, &status) != nil {
				return Plan{}, false, invalid("неверный status")
			}
		}
		if !hasID {
			var revision uint64
			if raw, exists := entry["revision"]; exists && json.Unmarshal(raw, &revision) != nil {
				return Plan{}, false, invalid("неверный revision")
			}
			if attempts || warnings || status != "" && status != Todo || revision > 1 {
				return Plan{}, false, invalid("задача без id содержит историю; сохраните её исходный id")
			}
			for {
				id, err := NewID()
				if err != nil {
					return Plan{}, false, err
				}
				if !ids[id] {
					fill(entry, "id", id)
					ids[id] = true
					break
				}
			}
		}
		if attempts || warnings || status != "" && status != Todo {
			if entry["revision"] == nil || entry["status"] == nil {
				return Plan{}, false, invalid("задача с историей требует status и revision")
			}
		} else {
			fill(entry, "status", Todo)
			fill(entry, "revision", uint64(1))
		}
	}
	fill(fields, "revision", uint64(0))
	fill(fields, "last_event", uint64(0))
	fill(fields, "order", []string{})
	var order []string
	if err := json.Unmarshal(fields["order"], &order); err != nil {
		return Plan{}, false, invalid("неверный order")
	}
	seen := map[string]bool{}
	for _, id := range order {
		if !ids[id] || seen[id] {
			return Plan{}, false, invalid("неизвестный или повторный ID в order: %q", id)
		}
		seen[id] = true
	}
	for _, entry := range entries {
		var id string
		_ = json.Unmarshal(entry["id"], &id)
		if !seen[id] {
			order = append(order, id)
			changed = true
		}
	}
	fields["order"], _ = json.Marshal(order)
	fields["tasks"], _ = json.Marshal(entries)
	completed, err := json.Marshal(fields)
	if err != nil {
		return Plan{}, false, err
	}
	var plan Plan
	if err := json.Unmarshal(completed, &plan); err != nil {
		return Plan{}, false, err
	}
	if changed {
		if plan.Revision == math.MaxUint64 {
			return Plan{}, false, invalid("исчерпан счётчик ревизий")
		}
		plan.Revision++
	}
	return plan, changed, nil
}

func QueueApplied(plan Plan) (Plan, error) {
	if plan.Team == nil {
		return plan, nil
	}
	plan.Tasks = slices.Clone(plan.Tasks)
	base, err := ValidateShared(plan.Team.Base)
	if err != nil {
		return Plan{}, err
	}
	expected, err := Replay(base, plan.Team.Pending)
	if err != nil {
		return Plan{}, err
	}
	if reflect.DeepEqual(SharedContent(expected), SharedContent(plan)) {
		return plan, nil
	}
	before := expected
	before.Team = plan.Team
	before.Actions = plan.Actions
	for i := range plan.Tasks {
		old, err := expected.FindID(plan.Tasks[i].ID)
		if err != nil {
			continue
		}
		a, b := Shared(Plan{Tasks: []Task{old}}).Tasks[0], Shared(Plan{Tasks: []Task{plan.Tasks[i]}}).Tasks[0]
		a.Revision, b.Revision = 0, 0
		if !reflect.DeepEqual(a, b) {
			revision := max(old.Revision, plan.Tasks[i].Revision)
			if revision == math.MaxUint64 {
				return Plan{}, invalid("исчерпан счётчик ревизий задачи")
			}
			plan.Tasks[i].Revision = revision + 1
		}
	}
	queued, err := Queue(before, plan)
	if err != nil {
		return Plan{}, err
	}
	if len(queued.Team.Pending) > len(plan.Team.Pending) {
		revision := max(plan.Revision, expected.Revision)
		if revision == math.MaxUint64 {
			return Plan{}, invalid("исчерпан счётчик ревизий")
		}
		queued.Revision = revision + 1
	}
	return queued, queued.Validate()
}

func SharedContent(plan Plan) Plan {
	plan = Shared(plan)
	plan.Actions = nil
	plan.Revision = 0
	for i := range plan.Tasks {
		plan.Tasks[i].Revision = 0
		plan.Tasks[i].ActiveAttempt = nil
	}
	slices.SortFunc(plan.Tasks, func(a, b Task) int { return cmp.Compare(a.ID, b.ID) })
	return plan
}
