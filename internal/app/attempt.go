package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"git-task/internal/task"
)

func (p *Plans) author(ctx context.Context) (string, error) {
	return p.git.Author(ctx)
}

func (p *Plans) selectAttempt(ctx context.Context, plan task.Plan, item task.Task, ids []string) (task.Attempt, error) {
	if len(ids) > 1 {
		return task.Attempt{}, fmt.Errorf("несколько селекторов подхода")
	}
	if len(ids) == 1 && ids[0] != "" {
		a, err := item.Attempt(ids[0])
		if err != nil {
			return a, err
		}
		if a.Status == task.Done {
			return a, task.ErrTransition
		}
		return a, nil
	}
	var matches []task.Attempt
	if item.Status == task.Todo || item.Status == task.Done || item.Status == task.Archived {
		return task.Attempt{}, task.ErrTransition
	}
	for _, a := range item.Attempts {
		if a.Status == task.Done {
			continue
		}
		if plan.Team != nil && !slices.Contains(plan.Team.LocalAttempts, a.ID) {
			continue
		}
		matches = append(matches, a)
	}
	if len(matches) != 1 {
		return task.Attempt{}, fmt.Errorf("%w: используйте --attempt <id>", task.ErrAmbiguous)
	}
	return matches[0], nil
}

func markLocal(plan *task.Plan, id string) {
	if plan.Team == nil {
		return
	}
	team := *plan.Team
	if slices.Contains(team.LocalAttempts, id) {
		return
	}
	team.LocalAttempts = append(slices.Clone(team.LocalAttempts), id)
	plan.Team = &team
}

func validateRemoteName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\ \x00\r\n:") {
		return fmt.Errorf("неверное имя remote")
	}
	return nil
}
