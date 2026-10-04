package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

func MigrateV1(data []byte) (Plan, error) {
	if !utf8.Valid(data) {
		return Plan{}, invalid("JSON не является UTF-8")
	}
	if err := checkUnicodeEscapes(data); err != nil {
		return Plan{}, err
	}
	if err := checkJSON(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return Plan{}, err
	}
	if err := legacyOnlyFields(data); err != nil {
		return Plan{}, err
	}
	type planFields Plan
	type taskFields Task
	var old struct {
		planFields
		Tasks []struct {
			taskFields
			Active *Attempt `json:"active_attempt,omitempty"`
		} `json:"tasks"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&old); err != nil {
		return Plan{}, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Plan{}, invalid("лишние данные JSON")
	}
	p := Plan(old.planFields)
	if old.Tasks == nil {
		return Plan{}, invalid("tasks должен быть массивом")
	}
	if p.Team != nil || len(p.Actions) != 0 {
		return Plan{}, invalid("общие поля в схеме 1")
	}
	p.Tasks = make([]Task, len(old.Tasks))
	for i, t := range old.Tasks {
		p.Tasks[i] = Task(t.taskFields)
		p.Tasks[i].ActiveAttempt = t.Active
		for _, a := range p.Tasks[i].Attempts {
			if a.Status != "" || a.Author != "" {
				return Plan{}, invalid("поля схемы 2 в схеме 1")
			}
		}
		if t.Active != nil && (t.Active.Status != "" || t.Active.Author != "") {
			return Plan{}, invalid("поля схемы 2 в схеме 1")
		}
	}
	if err := p.validateLegacy(); err != nil {
		return Plan{}, err
	}
	for i := range p.Tasks {
		t := &p.Tasks[i]
		for j := range t.Attempts {
			t.Attempts[j].Status = Done
		}
		if t.ActiveAttempt != nil {
			a := t.ActiveAttempt.clone()
			a.Status = Active
			if t.Status == Paused {
				a.Status = Paused
			}
			t.Attempts = append(t.Attempts, a)
			for i := range t.Warnings {
				t.Warnings[i].AttemptID = a.ID
			}
		}
		t.ActiveAttempt = nil
		t.Status = t.AggregateStatus()
		t.project("")
	}
	p.SchemaVersion = SchemaVersion
	if err := p.Validate(); err != nil {
		return Plan{}, fmt.Errorf("миграция: %w", err)
	}
	return p, nil
}

func legacyOnlyFields(data []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	for _, key := range []string{"team", "actions", "server_events"} {
		if _, ok := top[key]; ok {
			return invalid("поле схемы 2 в схеме 1: %s", key)
		}
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(top["tasks"], &tasks); err != nil {
		return err
	}
	for _, t := range tasks {
		var attempts []map[string]json.RawMessage
		if raw, ok := t["attempts"]; ok {
			if err := json.Unmarshal(raw, &attempts); err != nil {
				return err
			}
		}
		if raw, ok := t["active_attempt"]; ok {
			var a map[string]json.RawMessage
			if err := json.Unmarshal(raw, &a); err != nil {
				return err
			}
			attempts = append(attempts, a)
		}
		for _, a := range attempts {
			for _, key := range []string{"author", "status"} {
				if _, ok := a[key]; ok {
					return invalid("поле схемы 2 в подходе схемы 1: %s", key)
				}
			}
			if raw, ok := a["completion"]; ok {
				var c map[string]json.RawMessage
				if err := json.Unmarshal(raw, &c); err != nil {
					return err
				}
				if _, ok := c["target_before"]; ok {
					return invalid("target_before в схеме 1")
				}
			}
		}
		var warnings []map[string]json.RawMessage
		if raw, ok := t["warnings"]; ok {
			if err := json.Unmarshal(raw, &warnings); err != nil {
				return err
			}
		}
		for _, w := range warnings {
			if _, ok := w["attempt_id"]; ok {
				return invalid("attempt_id в предупреждении схемы 1")
			}
		}
	}
	return nil
}
