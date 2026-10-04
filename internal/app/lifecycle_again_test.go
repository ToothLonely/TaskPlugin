package app

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"git-task/internal/task"
)

func TestManualCompleteAgainPauseResumeAndMergeHistory(t *testing.T) {
	for _, selector := range []string{"id", "title"} {
		for _, ff := range []bool{false, true} {
			t.Run(selector+fmtBool(ff), func(t *testing.T) {
				p, c := startFixture(t)
				ctx := context.Background()
				preview, err := p.PrepareComplete(ctx, "task-001", "")
				if err != nil {
					t.Fatal(err)
				}
				manual, err := p.ApplyComplete(ctx, preview)
				if err != nil || manual.Status != task.Done || len(manual.Attempts) != 1 || manual.Attempts[0].Completion.Source != task.Manual {
					t.Fatalf("manual: %+v %v", manual, err)
				}
				old := manual.Attempts[0]
				options := StartOptions{Branch: "again", Again: true}
				if selector == "id" {
					options.ID = manual.ID
				} else {
					options.Title = manual.Title
				}
				started, err := p.Start(ctx, options)
				if err != nil || started.Status != task.Active || len(started.Attempts) != 2 || !reflect.DeepEqual(started.Attempts[0], old) || started.ActiveAttempt.ID == old.ID {
					t.Fatalf("again: %+v %v", started, err)
				}
				if empty := syncTask(t, p, manual.ID); empty.Status != task.Active || len(empty.Attempts) != 2 {
					t.Fatalf("old integration completed empty new attempt: %+v", empty)
				}
				if _, _, err := p.Pause(ctx, manual.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := p.Resume(ctx, manual.ID); err != nil {
					t.Fatal(err)
				}
				commitFile(t, c, "new-work.txt", "new attempt\n")
				if work := syncTask(t, p, manual.ID); work.Status != task.Active || len(work.Attempts) != 2 || work.ActiveAttempt.Observation.WorkCommit == "" {
					t.Fatalf("new work: %+v", work)
				}
				mergeBranch(t, c, options.Branch, ff)
				done := syncTask(t, p, manual.ID)
				if done.Status != task.Done || done.ActiveAttempt != nil || len(done.Attempts) != 2 || !reflect.DeepEqual(done.Attempts[0], old) || done.Attempts[1].ID != started.ActiveAttempt.ID || done.Attempts[1].Completion.Source != task.Merge {
					t.Fatalf("final merge history: %+v", done)
				}
				before := planBytes(t, c)
				if _, changed, err := p.Sync(ctx); err != nil || changed || !bytes.Equal(before, planBytes(t, c)) {
					t.Fatalf("duplicate finalization: %v %v", changed, err)
				}
			})
		}
	}
}
