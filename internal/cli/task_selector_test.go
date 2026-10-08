package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func taskPrefixFixture(t *testing.T) (*app.Plans, *git.Client) {
	t.Helper()
	p, c := selectionFixture(t)
	ctx := context.Background()
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := storage.New(c.Dir, repo.ExcludePath, c)
	base, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan := base.Plan
	ids := []string{"abcd1111", "abcd2222", "efab3333"}
	for i := range plan.Tasks {
		plan.Tasks[i].ID = ids[i]
	}
	plan.Order = ids
	plan.InsertionTail = ids[len(ids)-1]
	plan.Revision++
	if _, err := s.Save(ctx, base, plan); err != nil {
		t.Fatal(err)
	}
	return p, c
}

func prefixCLI(p *app.Plans, args ...string) (int, string, string) {
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) { return p, nil })
	return code, out.String(), diagnostic.String()
}

func TestTaskIDPrefixesAcrossCommands(t *testing.T) {
	p, c := taskPrefixFixture(t)
	commands := [][]string{
		{"show", "--id", "efab"},
		{"edit", "--id", "efab", "--title", "Updated"},
		{"add", "Inserted", "--before", "efab"},
		{"move", "--id", "efab", "--before", "abcd1"},
		{"start", "feature/live", "--id", "efab"},
		{"pause", "--id", "efab"},
		{"resume", "--id", "efab"},
		{"complete", "--id", "efab", "--yes"},
		{"archive", "--id", "efab"},
		{"add", "After", "--after", "abcd2"},
	}
	for _, args := range commands {
		var expectedDiagnostic string
		if args[0] == "complete" {
			expectedDiagnostic = "Завершение manual: [efab3333] \"Updated\"; commit: \n"
		}
		if code, out, diagnostic := prefixCLI(p, args...); code != 0 || diagnostic != expectedDiagnostic {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, diagnostic)
		}
	}
	for _, branch := range []string{"feature/attached", "feature/rebound"} {
		testrepo.Run(t, c, "branch", branch, "main")
	}
	for _, args := range [][]string{
		{"attach", "feature/attached", "--id", "abcd1"},
		{"attach", "feature/rebound", "--id", "abcd1", "--rebind"},
	} {
		if code, out, diagnostic := prefixCLI(p, args...); code != 0 || diagnostic != "" {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, diagnostic)
		}
	}
	var plan task.Plan
	if err := json.Unmarshal(selectionBytes(t, c), &plan); err != nil {
		t.Fatal(err)
	}
	item, err := plan.FindID("efab3333")
	if err != nil || item.Status != task.Archived || item.Title != "Updated" || len(item.Attempts) != 1 || item.Attempts[0].Status != task.Done {
		t.Fatalf("full task ID/history changed: %+v err=%v", item, err)
	}
	completion := item.Attempts[0].Completion
	if completion == nil || completion.Source != task.Manual || completion.Commit != "" {
		t.Fatalf("manual completion without --commit changed: %+v", completion)
	}
	attached, err := plan.FindID("abcd1111")
	if err != nil || len(attached.Attempts) != 1 || attached.Attempts[0].Branch != "feature/rebound" {
		t.Fatalf("attach prefix was not resolved: %+v err=%v", attached, err)
	}
	for _, id := range plan.Order {
		if _, err := plan.FindID(id); err != nil {
			t.Fatalf("stored abbreviated ID %q: %v", id, err)
		}
	}
}

func TestAmbiguousTaskIDPrefixesPreservePlanAndGit(t *testing.T) {
	p, c := taskPrefixFixture(t)
	before := selectionBytes(t, c)
	refs := testrepo.Run(t, c, "show-ref")
	head := testrepo.Run(t, c, "rev-parse", "HEAD")
	for _, args := range [][]string{
		{"show", "--id", "abcd"},
		{"start", "feature/rejected", "--id", "abcd"},
		{"attach", "feature/rejected", "--id", "abcd"},
		{"edit", "--id", "abcd", "--title", "Rejected"},
		{"move", "--id", "abcd", "--end"},
		{"pause", "--id", "abcd"},
		{"resume", "--id", "abcd"},
		{"complete", "--id", "abcd", "--yes"},
		{"archive", "--id", "abcd"},
		{"add", "Rejected", "--before", "abcd"},
		{"add", "Rejected", "--after", "abcd"},
		{"move", "--id", "efab", "--after", "abcd"},
	} {
		code, out, diagnostic := prefixCLI(p, args...)
		if code != 1 || out != "" || !strings.Contains(diagnostic, "abcd1111, abcd2222") {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, diagnostic)
		}
		if !bytes.Equal(selectionBytes(t, c), before) || !bytes.Equal(testrepo.Run(t, c, "show-ref"), refs) || !bytes.Equal(testrepo.Run(t, c, "rev-parse", "HEAD"), head) {
			t.Fatalf("ambiguous selection changed plan or Git: %v", args)
		}
	}
}
