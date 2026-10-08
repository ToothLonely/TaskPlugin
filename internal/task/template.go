package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

type Template struct {
	TargetBranch *string        `json:"target_branch"`
	Tasks        []TemplateTask `json:"tasks"`
}

type TemplateTask struct {
	Title       string  `json:"title"`
	ID          *string `json:"id,omitempty"`
	Description string  `json:"description,omitempty"`
}

func ReadTemplate(data []byte) (Template, error) {
	var result Template
	if !utf8.Valid(data) {
		return result, invalid("JSON не является UTF-8")
	}
	if err := checkUnicodeEscapes(data); err != nil {
		return result, err
	}
	if err := checkJSON(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return result, fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return Template{}, fmt.Errorf("%w: шаблон JSON: %v", ErrInvalid, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Template{}, invalid("лишние данные после JSON")
	}
	if err := result.validate(); err != nil {
		return Template{}, err
	}
	return result, nil
}

func (t Template) validate() error {
	if t.TargetBranch == nil || t.Tasks == nil {
		return invalid("шаблон требует target_branch и массив tasks")
	}
	if *t.TargetBranch != "" && !validBranch(*t.TargetBranch) {
		return invalid("недопустимая target_branch")
	}
	ids := map[string]bool{}
	for i, entry := range t.Tasks {
		if blank(entry.Title) || !utf8.ValidString(entry.Title) || !utf8.ValidString(entry.Description) {
			return invalid("tasks[%d] требует непустой title и текст в UTF-8", i)
		}
		if entry.ID != nil {
			if !identity(*entry.ID) || ids[*entry.ID] {
				return invalid("недопустимый или повторный ID %q", *entry.ID)
			}
			ids[*entry.ID] = true
		}
	}
	return nil
}

func (t Template) Plan() (Plan, error) {
	if err := t.validate(); err != nil {
		return Plan{}, err
	}
	plan, err := NewPlan(*t.TargetBranch)
	if err != nil {
		return Plan{}, err
	}
	ids := map[string]bool{}
	for _, entry := range t.Tasks {
		if entry.ID != nil {
			ids[*entry.ID] = true
		}
	}
	for _, entry := range t.Tasks {
		var id string
		if entry.ID != nil {
			id = *entry.ID
		} else {
			for {
				id, err = NewID()
				if err != nil {
					return Plan{}, err
				}
				if !ids[id] {
					break
				}
			}
		}
		ids[id] = true
		plan.Tasks = append(plan.Tasks, Task{ID: id, Title: entry.Title, Description: entry.Description, Status: Todo})
		plan.Order = append(plan.Order, id)
		plan.InsertionTail = id
	}
	return plan, plan.Validate()
}
