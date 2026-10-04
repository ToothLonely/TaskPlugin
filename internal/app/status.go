package app

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"git-task/internal/task"
)

type StatusRepository struct {
	Root            string     `json:"root"`
	TargetBranch    string     `json:"target_branch"`
	CurrentBranch   *string    `json:"current_branch"`
	HeadCommit      *string    `json:"head_commit"`
	HeadCommittedAt *time.Time `json:"head_committed_at"`
	Detached        bool       `json:"detached"`
	Unborn          bool       `json:"unborn"`
}

type Progress struct {
	Done    int      `json:"done"`
	Total   int      `json:"total"`
	Percent *float64 `json:"percent"`
}

type StatusWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TaskID  string `json:"task_id,omitempty"`
}

type LastCompletion struct {
	TaskID     string          `json:"task_id"`
	TaskStatus task.Status     `json:"task_status"`
	AttemptID  string          `json:"attempt_id"`
	Completion task.Completion `json:"completion"`
}

type StatusReport struct {
	SchemaVersion  int              `json:"schema_version"`
	Repository     StatusRepository `json:"repository"`
	Progress       Progress         `json:"progress"`
	Tasks          []task.Task      `json:"tasks"`
	CurrentTaskID  *string          `json:"current_task_id"`
	LastCompletion *LastCompletion  `json:"last_completion"`
	NextTaskID     *string          `json:"next_task_id"`
	Warnings       []StatusWarning  `json:"warnings"`
}

func statusReport(plan task.Plan, repo StatusRepository, pending bool) (StatusReport, error) {
	r := StatusReport{SchemaVersion: 2, Repository: repo, Tasks: []task.Task{}, Warnings: []StatusWarning{}}
	for _, id := range plan.Order {
		t, err := plan.FindID(id)
		if err != nil {
			return StatusReport{}, err
		}
		r.Tasks = append(r.Tasks, t)
		if t.Status != task.Archived {
			r.Progress.Total++
		}
		if t.Status == task.Done {
			r.Progress.Done++
		}
		for _, a := range t.Attempts {
			if plan.Team != nil && !slices.Contains(plan.Team.LocalAttempts, a.ID) {
				continue
			}
			if t.Status != task.Archived && a.Status != task.Done && repo.CurrentBranch != nil && a.Branch == *repo.CurrentBranch {
				current := id
				r.CurrentTaskID = &current
			}
		}
		for _, a := range t.Attempts {
			if a.Completion != nil && (r.LastCompletion == nil || a.Completion.Event > r.LastCompletion.Completion.Event) {
				r.LastCompletion = &LastCompletion{TaskID: id, TaskStatus: t.Status, AttemptID: a.ID, Completion: *a.Completion}
			}
		}
		for _, w := range t.Warnings {
			r.Warnings = append(r.Warnings, StatusWarning{Code: w.Code, Message: w.Message, TaskID: id})
		}
	}
	if r.Progress.Total != 0 {
		percent := math.Round(1000*float64(r.Progress.Done)/float64(r.Progress.Total)) / 10
		r.Progress.Percent = &percent
	}
	next, err := plan.FirstTodo()
	if err == nil {
		r.NextTaskID = &next.ID
	} else if !errors.Is(err, task.ErrNotFound) {
		return StatusReport{}, err
	}
	if plan.Team != nil && len(plan.Team.Pending) > 0 {
		r.Warnings = append(r.Warnings, StatusWarning{Code: "unpublished", Message: "Локальные действия ещё не опубликованы; выполните git task team publish."})
	}
	if pending {
		r.Warnings = append(r.Warnings, StatusWarning{Code: "operation_pending", Message: "Незавершённая операция в operation.json; изменения заблокированы до явного восстановления."})
	}
	return r, nil
}

func (p *Plans) StatusReport(ctx context.Context) (StatusReport, error) {
	plan, pending, err := p.StatusState(ctx)
	if err != nil && !errors.Is(err, ErrReceive) {
		return StatusReport{}, err
	}
	receiveErr := err
	head, err := p.git.StatusHead(ctx)
	if err != nil {
		return StatusReport{}, err
	}
	report, err := statusReport(plan, StatusRepository{Root: p.git.Dir, TargetBranch: plan.TargetBranch, CurrentBranch: head.Branch, HeadCommit: head.Commit, HeadCommittedAt: head.CommittedAt, Detached: head.Detached, Unborn: head.Unborn}, pending)
	if receiveErr != nil {
		report.Warnings = append(report.Warnings, StatusWarning{Code: "shared_receive_failed", Message: receiveErr.Error()})
	}
	return report, err
}

func (p *Plans) Show(ctx context.Context, id string) (task.Task, bool, error) {
	snapshot, err := p.store.Load(ctx)
	if err != nil {
		return task.Task{}, false, err
	}
	item, err := snapshot.Plan.FindID(id)
	return item, snapshot.PendingOperation, err
}
