package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"git-task/internal/app"
)

func TestHooksArgumentsAndHelp(t *testing.T) {
	t.Setenv("PATH", "")
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"hooks", "--help"}, 0},
		{[]string{"hooks", "install", "--help"}, 0},
		{[]string{"hooks", "--help", "uninstall"}, 0},
		{[]string{"help", "hooks"}, 0},
		{[]string{"hooks"}, 2},
		{[]string{"hooks", "install", "extra"}, 2},
		{[]string{"hooks", "install", "--force"}, 2},
		{[]string{"hooks", "--help=false"}, 2},
		{[]string{"hooks", "--help", "--help"}, 2},
		{[]string{"hooks", "--", "--help"}, 2},
	} {
		var out, diagnostic bytes.Buffer
		code := Run(context.Background(), tc.args, "test", Streams{Out: &out, Err: &diagnostic})
		if code != tc.code || tc.code == 0 && diagnostic.Len() != 0 || tc.code != 0 && out.Len() != 0 {
			t.Fatalf("%q: code=%d out=%q diagnostic=%q", tc.args, code, &out, &diagnostic)
		}
	}
}

func TestHookFailureWarnsAndNeverPrompts(t *testing.T) {
	t.Setenv("GIT_TASK_OPERATION", "")
	for _, args := range [][]string{{"_hook"}, {"_hook", "unknown"}, {"_hook", "post-commit"}, {"_hook", "post-rewrite", "amend"}} {
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(context.Background(), args, "test", Streams{
			In: strings.NewReader("broken\n"), Out: &out, Err: &diagnostic,
			OpenDialogue: func() (Dialogue, error) { t.Fatal("hook prompted"); return nil, nil },
		}, func(context.Context) (*app.Plans, error) { return nil, errors.New("storage unavailable") })
		if code != 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "Git-операция уже произошла") {
			t.Fatalf("%q: code=%d out=%q diagnostic=%q", args, code, &out, &diagnostic)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var diagnostic bytes.Buffer
	if code := RunWithPlans(ctx, []string{"_hook", "post-commit"}, "test", Streams{Err: &diagnostic}, nil); code != 0 {
		t.Fatalf("cancelled hook: %d", code)
	}
}

func TestHookOperationGuardSkipsAllInput(t *testing.T) {
	t.Setenv("GIT_TASK_OPERATION", "owned-operation")
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"_hook", "post-rewrite", "amend"}, "test", Streams{
		In: unexpectedReader{t}, Out: &out, Err: &diagnostic,
	}, func(context.Context) (*app.Plans, error) { t.Fatal("guard opened plan"); return nil, nil })
	if code != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatalf("guard: code=%d out=%q diagnostic=%q", code, &out, &diagnostic)
	}
}

type unexpectedReader struct{ t *testing.T }

func (r unexpectedReader) Read([]byte) (int, error) {
	r.t.Fatal("unexpected stdin read")
	return 0, nil
}
