package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/git"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func localizedTeamFixture(t *testing.T) (*app.Plans, *git.Client) {
	t.Helper()
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "")
	p, c := selectionFixture(t)
	remote := filepath.Join(t.TempDir(), "общий план.git")
	testrepo.Run(t, c, "init", "--bare", "--initial-branch=main", remote)
	testrepo.Run(t, c, "remote", "add", "origin", remote)
	if err := p.Connect(context.Background(), "origin"); err != nil {
		t.Fatal(err)
	}
	return p, c
}

func TestLocalizedReviewSharedReceiveFailure(t *testing.T) {
	p, c := localizedTeamFixture(t)
	ctx := context.Background()
	tip, err := c.PlanCommit(ctx, "", []byte(`{"format":"git-task","schema_version":99}`))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "update-ref", "refs/remotes/origin/git-task-plan", tip)
	before := selectionBytes(t, c)
	var out, errOut bytes.Buffer
	code := RunWithPlans(ctx, []string{"status", "--json"}, "test", Streams{Out: &out, Err: &errOut}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 0 || !json.Valid(out.Bytes()) || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("code=%d stdout=%q stderr=%q; plan preserved=%t", code, &out, &errOut, bytes.Equal(before, selectionBytes(t, c)))
	}
	var report struct {
		Warnings []map[string]json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range report.Warnings {
		if string(warning["code"]) != `"shared_receive_failed"` {
			continue
		}
		found = true
		var message string
		if err := json.Unmarshal(warning["message"], &message); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"shared version not applied", "received version preserved in", c.Dir, "invalid plan data"} {
			if !strings.Contains(message, want) {
				t.Errorf("warning %q does not contain %q", message, want)
			}
		}
		if len(warning) != 2 || !strings.Contains(errOut.String(), "shared version not applied") {
			t.Fatalf("internal cause leaked or stderr not localized: warning=%v stderr=%q", warning, &errOut)
		}
	}
	if !found {
		t.Fatalf("missing receive warning: %s", out.Bytes())
	}
}

func TestLocalizedReviewPublicationFailurePreservesQueue(t *testing.T) {
	p, c := localizedTeamFixture(t)
	missing := filepath.Join(t.TempDir(), "недоступный remote.git")
	testrepo.Run(t, c, "remote", "set-url", "origin", missing)
	var out, errOut bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"add", "задача не найдена"}, "test", Streams{Out: &out, Err: &errOut}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 0 || !strings.Contains(errOut.String(), "Warning: publication unconfirmed, local queue preserved:") || !strings.Contains(errOut.String(), "exited with code") || !strings.Contains(errOut.String(), "git task team publish") || !strings.Contains(errOut.String(), missing) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, &out, &errOut)
	}
	var saved task.Plan
	if err := json.Unmarshal(selectionBytes(t, c), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Team == nil || len(saved.Team.Pending) != 1 || len(saved.Tasks) != 4 || saved.Tasks[3].Title != "задача не найдена" {
		t.Fatalf("local task or publication queue changed: %+v", saved)
	}
}
