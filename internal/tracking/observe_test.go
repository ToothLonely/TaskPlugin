package tracking

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/task"
)

type failingReader struct {
	Reader
	base          string
	work          string
	logError      error
	objectMissing bool
	ancestryError error
}

func (r failingReader) BranchCommit(_ context.Context, _ string) (string, error) {
	return r.work, nil
}

func (r failingReader) BranchLog(_ context.Context, branch string) (git.RefLog, error) {
	if r.logError != nil {
		return git.RefLog{}, r.logError
	}
	if branch == "work" {
		return git.RefLog{Entries: []git.RefUpdate{{Old: r.base, New: r.work, Digest: strings.Repeat("c", 64)}}}, nil
	}
	return git.RefLog{Entries: []git.RefUpdate{{Old: strings.Repeat("0", 40), New: r.base, Digest: strings.Repeat("d", 64)}, {Old: r.base, New: r.work, Digest: strings.Repeat("e", 64)}}}, nil
}

func (r failingReader) HasObject(_ context.Context, _ string) (bool, error) {
	return !r.objectMissing, nil
}

func (r failingReader) IsAncestor(_ context.Context, _, _ string) (bool, error) {
	return false, r.ancestryError
}

func TestTrackingErrorsAndInsufficientHistoryNeverComplete(t *testing.T) {
	base, work := strings.Repeat("a", 40), strings.Repeat("b", 40)
	a := task.Attempt{ID: "attempt", Branch: "work", TargetBranch: "main", BaseCommit: base,
		Observation: &task.Observation{Tip: work, TargetCommit: base, WorkCommit: work, BranchLog: strings.Repeat("c", 64), TargetLog: strings.Repeat("d", 64)}}
	failure := errors.New("Git failed")
	for _, tc := range []struct {
		name      string
		reader    failingReader
		wantError error
		wantCode  string
	}{
		{"git error", failingReader{base: base, work: work, ancestryError: failure}, failure, ""},
		{"missing object", failingReader{base: base, work: work, objectMissing: true}, nil, "history_unavailable"},
		{"history budget", failingReader{base: base, work: work, logError: git.ErrHistoryUnavailable}, nil, "history_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Observe(context.Background(), tc.reader, a, time.Now().UTC())
			if !errors.Is(err, tc.wantError) || r.Completion != nil {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			if tc.wantCode != "" && (len(r.Warnings) != 1 || r.Warnings[0].Code != tc.wantCode) {
				t.Fatalf("missing warning: %+v", r)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := Observe(ctx, nil, a, time.Now()); !errors.Is(err, context.Canceled) || r.Completion != nil {
		t.Fatalf("cancel: %+v %v", r, err)
	}
	r := Result{tip: base, target: work}
	if err := r.Recheck(context.Background(), failingReader{base: base, work: work}, a); err == nil {
		t.Fatal("changed refs accepted before save")
	}
}

func TestBindingContinuityRequiresExistingPrefixAndMatchingRef(t *testing.T) {
	base, tip := strings.Repeat("a", 40), strings.Repeat("b", 40)
	digest := strings.Repeat("c", 64)
	a := task.Attempt{ID: "current", BaseCommit: base,
		Observation: &task.Observation{Tip: base, BranchLog: digest}}
	initial := git.RefUpdate{New: base, Message: "git-task start current", Digest: digest}
	advanced := git.RefUpdate{Old: base, New: tip, Digest: strings.Repeat("d", 64)}
	for _, tc := range []struct {
		name    string
		a       task.Attempt
		entries []git.RefUpdate
		tip     string
		want    bool
	}{
		{"unchanged", a, []git.RefUpdate{initial}, base, true},
		{"advanced", a, []git.RefUpdate{initial, advanced}, tip, true},
		{"missing log", a, nil, base, false},
		{"lost prefix", a, []git.RefUpdate{advanced}, tip, false},
		{"unlogged ref", a, []git.RefUpdate{initial}, tip, false},
		{"broken chain", a, []git.RefUpdate{initial, {Old: tip, New: tip, Digest: advanced.Digest}}, tip, false},
		{"legacy marker", task.Attempt{ID: "current", BaseCommit: base}, []git.RefUpdate{initial, advanced}, tip, true},
		{"foreign marker", task.Attempt{ID: "other", BaseCommit: base}, []git.RefUpdate{initial}, base, false},
		{"rebound marker", task.Attempt{ID: "current", BaseCommit: base, Rebindings: []task.Rebinding{{From: "work", To: "work"}}}, []git.RefUpdate{initial}, base, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BindingContinuous(tc.a, git.RefLog{Entries: tc.entries}, tc.tip); got != tc.want {
				t.Fatalf("continuous=%v, want %v", got, tc.want)
			}
		})
	}
}
