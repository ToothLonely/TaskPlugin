package app

import "git-task/internal/task"

func statusIndexes(plan task.Plan) (map[string]task.Task, map[string]bool) {
	tasks := plan.TaskSnapshots()
	locals := map[string]bool{}
	if plan.Team != nil {
		for _, id := range plan.Team.LocalAttempts {
			locals[id] = true
		}
	}
	return tasks, locals
}
