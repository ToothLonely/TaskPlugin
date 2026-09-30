package cli

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/testrepo"
)

func TestSyncCompleteArgumentsAndHelp(t *testing.T) {
	for _, args := range [][]string{
		{"sync", "extra"}, {"sync", "--json"}, {"sync", "--help", "--help"},
		{"complete"}, {"complete", "extra"}, {"complete", "--id"}, {"complete", "--id="},
		{"complete", "--id=x", "--id=x"}, {"complete", "--id=x", "--commit"},
		{"complete", "--id=x", "--yes=false"}, {"complete", "--id=x", "--unknown"},
		{"complete", "--help", "--", "extra"}, {"complete", "--id=x", "--yes", "--", "extra"},
		{"complete", "--", "--help"}, {"complete", "--id=x", "--", "--yes"},
	} {
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) {
			t.Fatal("repository opened for invalid arguments")
			return nil, nil
		})
		if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("%v: %d %q %q", args, code, &out, &diagnostic)
		}
	}
	t.Setenv("PATH", "")
	for _, args := range [][]string{{"sync", "--help"}, {"help", "sync"}, {"complete", "--help"}, {"complete", "--help", "--"}, {"help", "complete"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}); code != 0 || !strings.Contains(out.String(), "Использование:") || diagnostic.Len() != 0 {
			t.Fatalf("help %v: %d %q %q", args, code, &out, &diagnostic)
		}
	}
}

func TestCompleteTrailingSeparatorAndNoOp(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	var out, diagnostic bytes.Buffer
	args := []string{"complete", "--id", "task-001", "--yes", "--"}
	if code := RunWithPlans(ctx, args, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 || !strings.Contains(out.String(), "done") || !strings.Contains(diagnostic.String(), "manual") {
		t.Fatalf("complete with separator: %d %q %q", code, &out, &diagnostic)
	}
	before := selectionBytes(t, c)
	out.Reset()
	diagnostic.Reset()
	if code := RunWithPlans(ctx, args, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 || diagnostic.Len() != 0 || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("no-op with separator: %d %q", code, &diagnostic)
	}
}

func TestCompleteConfirmationCancellationAndSavedOutputFailure(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	before := selectionBytes(t, c)
	for _, input := range []string{"нет\n", "", "\n"} {
		var out, diagnostic bytes.Buffer
		dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader(input))}
		code := RunWithPlans(ctx, []string{"complete", "--id=task-001"}, "test", Streams{Out: &out, Err: &diagnostic, OpenDialogue: func() (Dialogue, error) { return dialogue, nil }}, open)
		if code != 130 || !dialogue.closed || !bytes.Equal(before, selectionBytes(t, c)) || !strings.Contains(diagnostic.String(), "manual") {
			t.Fatalf("cancel: %d %q", code, &diagnostic)
		}
	}
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(ctx, []string{"complete", "--id=task-001"}, "test", Streams{Out: &out, Err: &diagnostic}, open)
	if code != 1 || !strings.Contains(diagnostic.String(), "--yes") || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("no terminal: %d %q", code, &diagnostic)
	}
	base := string(bytes.TrimSpace(testrepo.Run(t, c, "rev-parse", "HEAD")))
	diagnostic.Reset()
	code = RunWithPlans(ctx, []string{"complete", "--id=task-001", "--commit=" + base, "--yes"}, "test", Streams{Out: failedWriter{}, Err: &diagnostic}, open)
	if code != 1 || !strings.Contains(diagnostic.String(), "уже завершена") || !bytes.Contains(selectionBytes(t, c), []byte(`"source": "manual"`)) {
		t.Fatalf("saved output failure: %d %q", code, &diagnostic)
	}
	before = selectionBytes(t, c)
	out.Reset()
	diagnostic.Reset()
	if code := RunWithPlans(ctx, []string{"complete", "--id=task-001"}, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 || diagnostic.Len() != 0 || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("repeat no-op: %d %q", code, &diagnostic)
	}
	dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader("да\n"))}
	if code := RunWithPlans(ctx, []string{"complete", "--id=task-002"}, "test", Streams{Out: &out, Err: &diagnostic, OpenDialogue: func() (Dialogue, error) { return dialogue, nil }}, open); code != 0 || !dialogue.closed {
		t.Fatalf("confirmed: %d %q", code, &diagnostic)
	}
}

func TestSyncCLIAndStatusShareTracking(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	if _, err := p.Start(ctx, app.StartOptions{Branch: "work"}); err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(ctx, args, "test", Streams{Out: &out, Err: &diagnostic}, open)
		return code, out.String(), diagnostic.String()
	}
	if code, out, diagnostic := run("sync"); code != 0 || !strings.Contains(out, "обновлён") || diagnostic != "" {
		t.Fatalf("observe: %d %q %q", code, out, diagnostic)
	}
	testrepo.Run(t, c, "checkout", "main")
	testrepo.Run(t, c, "merge", "--ff-only", "work")
	if code, out, diagnostic := run("status"); code != 0 || !strings.Contains(out, "done") || diagnostic != "" {
		t.Fatalf("status: %d %q %q", code, out, diagnostic)
	}
	before := selectionBytes(t, c)
	if code, out, diagnostic := run("sync"); code != 0 || !strings.Contains(out, "изменений нет") || diagnostic != "" || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("repeat: %d %q %q", code, out, diagnostic)
	}
}
