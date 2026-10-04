package task

import (
	"slices"
)

func ApplyLocal(shared, local Plan) Plan {
	result := shared
	result.Team = local.Team
	result.ServerEvents = slices.Clone(local.ServerEvents)
	result.Tasks = slices.Clone(shared.Tasks)
	for i := range result.Tasks {
		t := &result.Tasks[i]
		*t = t.clone()
		old, err := local.FindID(t.ID)
		if err != nil {
			continue
		}
		t.Warnings = slices.Clone(old.Warnings)
		filtered := t.Warnings[:0]
		for _, w := range t.Warnings {
			if w.Code != "parallel_work" && w.Code != "late_attempt" {
				filtered = append(filtered, w)
			}
		}
		t.Warnings = filtered
		live := 0
		for _, a := range t.Attempts {
			if a.Status != Done {
				live++
			}
		}
		if live > 1 {
			t.Warnings = append(t.Warnings, Warning{Code: "parallel_work", Message: "В задаче несколько независимых незавершённых подходов."})
		}
		if old.Status == Done && t.Status == Active {
			t.Warnings = append(t.Warnings, Warning{Code: "late_attempt", Message: "Получен ранее неизвестный подход: все известные подходы ещё не завершены, задача снова active."})
		}
		for j := range t.Attempts {
			a := &t.Attempts[j]
			previous, err := old.Attempt(a.ID)
			if err == nil && a.Branch == previous.Branch && a.BaseCommit == previous.BaseCommit {
				a.Observation = previous.Observation
			}
		}
	}
	result.Revision = max(result.Revision, local.Revision) + 1
	return result
}
