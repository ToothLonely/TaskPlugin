package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"git-task/internal/task"
)

func selectTaskID(plan task.Plan, selector string) (task.Task, error) {
	item, err := plan.FindID(selector)
	if err == nil || !errors.Is(err, task.ErrNotFound) {
		return item, err
	}
	if strings.TrimSpace(selector) == "" {
		return task.Task{}, err
	}
	var matches []string
	for _, candidate := range plan.Tasks {
		if strings.HasPrefix(candidate.ID, selector) {
			matches = append(matches, candidate.ID)
		}
	}
	switch len(matches) {
	case 0:
		return task.Task{}, err
	case 1:
		return plan.FindID(matches[0])
	default:
		slices.Sort(matches)
		return task.Task{}, fmt.Errorf("%w: префикс ID %q совпадает с задачами %s; укажите более длинный или полный ID", task.ErrAmbiguous, selector, strings.Join(matches, ", "))
	}
}

func selectPosition(plan task.Plan, position task.Position) (task.Position, error) {
	for _, selector := range []*string{&position.Before, &position.After} {
		if *selector == "" {
			continue
		}
		item, err := selectTaskID(plan, *selector)
		if err != nil {
			return task.Position{}, err
		}
		*selector = item.ID
	}
	return position, nil
}
