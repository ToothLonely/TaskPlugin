package app

import (
	"context"
	"errors"
	"testing"

	"git-task/internal/task"
)

func TestRecordedActionWithUnclosedJournalCannotPublish(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	command := f.a.ForCommand()
	failure := errors.New("interrupted after plan commit")
	command.checkpoint = func(point string) error {
		if point == "committed" {
			return failure
		}
		return nil
	}
	if _, err := command.Start(ctx, StartOptions{Branch: "journal-pending", ID: f.id}); !errors.Is(err, failure) {
		t.Fatalf("expected committed interruption: %v", err)
	}
	before, err := f.a.store.Load(ctx)
	if err != nil || !before.PendingOperation || len(command.actionIDs) != 1 || len(before.Plan.Team.Pending) != 1 || before.Plan.Tasks[0].Status != task.Active {
		t.Fatalf("missing installed result or journal: %v", err)
	}
	remoteBefore := string(teamNetwork(t, f.ca, "ls-remote", "origin", "refs/heads/git-task-plan"))
	if err := command.PublishRecordedActions(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := f.a.store.Load(ctx)
	remoteAfter := string(teamNetwork(t, f.ca, "ls-remote", "origin", "refs/heads/git-task-plan"))
	if err != nil || !before.SameVersion(after) || !after.PendingOperation || remoteBefore != remoteAfter {
		t.Fatalf("unclosed journal permitted publication: %v", err)
	}
}
