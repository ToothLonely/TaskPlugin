package task

import "time"

// Source distinguishes observed integration from explicit or imported completion.
type Source string

const (
	Merge    Source = "merge"
	Manual   Source = "manual"
	Imported Source = "imported"
)

// MergeKind describes the integration evidence supplied by tracking.
type MergeKind string

const (
	MergeCommit MergeKind = "merge_commit"
	FastForward MergeKind = "fast_forward"
)

// Attempt is active when Completion is nil; only completed attempts enter history.
type Attempt struct {
	ID             string      `json:"id"`
	Branch         string      `json:"branch,omitempty"`
	OriginalBranch string      `json:"original_branch,omitempty"`
	TargetBranch   string      `json:"target_branch,omitempty"`
	BaseCommit     string      `json:"base_commit,omitempty"`
	StartedAt      *time.Time  `json:"started_at,omitempty"`
	Rebindings     []Rebinding `json:"rebindings,omitempty"`
	Completion     *Completion `json:"completion,omitempty"`
}

// Rebinding preserves the previous binding and records its replacement baseline.
type Rebinding struct {
	From       string    `json:"from"`
	To         string    `json:"to"`
	BaseCommit string    `json:"base_commit"`
	ObservedAt time.Time `json:"observed_at"`
}

// Completion separates registration order, discovery time and known actual time.
// Git evidence is structurally checked here; tracking must establish its truth.
type Completion struct {
	Event        uint64     `json:"event"`
	Source       Source     `json:"source"`
	TargetBranch string     `json:"target_branch"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ObservedAt   *time.Time `json:"observed_at,omitempty"`
	Commit       string     `json:"commit,omitempty"`
	MergeKind    MergeKind  `json:"merge_kind,omitempty"`
	WorkCommit   string     `json:"work_commit,omitempty"`
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func (a Attempt) clone() Attempt {
	a.StartedAt = cloneTime(a.StartedAt)
	a.Rebindings = append([]Rebinding(nil), a.Rebindings...)
	if a.Completion != nil {
		c := *a.Completion
		c.CompletedAt = cloneTime(c.CompletedAt)
		c.ObservedAt = cloneTime(c.ObservedAt)
		a.Completion = &c
	}
	return a
}
