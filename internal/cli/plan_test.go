package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/testrepo"
)

func TestPlanArgumentErrorsBeforeOpeningRepository(t *testing.T) {
	for _, args := range [][]string{
		{"init", "extra"}, {"init", "--target"}, {"init", "--target=main", "--target", "other"},
		{"init", "--target="}, {"status", "extra"}, {"status", "--json"}, {"add"}, {"add", " "},
		{"add", "one", "two"}, {"add", "x", "--description"}, {"add", "x", "--end=false"},
		{"add", "x", "--before=a", "--after=b"}, {"add", "x", "--end", "--end"},
		{"add", "x", "--after="}, {"add", "x", "--help", "--help"}, {"init", "--wat"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			open := func(context.Context) (*app.Plans, error) {
				t.Fatal("opened repository on syntax error")
				return nil, nil
			}
			code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, open)
			if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
				t.Fatalf("code=%d out=%q err=%q", code, &out, &diagnostic)
			}
		})
	}
}

func TestPlanHelpWithoutGit(t *testing.T) {
	t.Setenv("PATH", "")
	for _, command := range []string{"init", "add", "status"} {
		for _, args := range [][]string{{"help", command}, {command, "--help"}} {
			var out, diagnostic bytes.Buffer
			if code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}); code != 0 || !strings.Contains(out.String(), "Использование:") || diagnostic.Len() != 0 {
				t.Fatalf("%q: %d %q %q", args, code, &out, &diagnostic)
			}
		}
	}
}

func TestPlanCommandsFromNestedDirectory(t *testing.T) {
	c := testrepo.New(t)
	// app.Open uses the process environment. Set only the isolated test values;
	// all Git overrides are removed before installing the fixture environment.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, entry := range c.Env {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") || key == "HOME" || key == "USERPROFILE" || key == "XDG_CONFIG_HOME" {
			t.Setenv(key, value)
		}
	}
	nested := filepath.Join(c.Dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	open := func(ctx context.Context) (*app.Plans, error) { return app.Open(ctx, nested) }
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, open)
		return code, out.String(), diagnostic.String()
	}
	if code, out, err := run("init"); code != 0 || !strings.Contains(out, "План создан") || !strings.Contains(err, "ещё нет commit") {
		t.Fatalf("init: %d %q %q", code, out, err)
	}
	if code, out, err := run("add", "Первая", "--description", "Описание"); code != 0 || !strings.Contains(out, "task-001") || err != "" {
		t.Fatalf("add: %d %q %q", code, out, err)
	}
	if code, out, err := run("add", "--before", "task-001", "Вторая"); code != 0 || err != "" {
		t.Fatalf("add before: %d %q %q", code, out, err)
	}
	if code, out, err := run("status"); code != 0 || !strings.Contains(out, "todo") || strings.Index(out, "Вторая") > strings.Index(out, "Первая") || err != "" {
		t.Fatalf("status: %d %q %q", code, out, err)
	}
	if code, _, _ := run("init", "--target", "other"); code != 1 {
		t.Fatalf("target change=%d", code)
	}
	if code, _, _ := run("add", "bad", "--after", "missing"); code != 1 {
		t.Fatalf("missing anchor=%d", code)
	}
	var failedDiagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"add", "Успешно сохранена"}, "test", Streams{Out: failedWriter{}, Err: &failedDiagnostic}, open)
	if code != 1 || !strings.Contains(failedDiagnostic.String(), "task-003 уже добавлена") {
		t.Fatalf("write failure: %d %q", code, &failedDiagnostic)
	}
	plans, err := open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := plans.Status(context.Background())
	if err != nil || len(stored.Tasks) != 3 {
		t.Fatalf("output failure lost commit: %v %v", stored, err)
	}
	planPath := filepath.Join(c.Dir, ".git-task", "plan.json")
	if _, err := os.Stat(planPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(nested, ".git-task")); !os.IsNotExist(err) {
		t.Fatalf("metadata in nested cwd: %v", err)
	}
	if err := os.WriteFile(planPath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, out, err := run("status"); code != 1 || out != "" || err == "" {
		t.Fatalf("corrupt: %d %q %q", code, out, err)
	}
}

func TestPlanArgumentValuesAndSeparators(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		title, description string
	}{
		{[]string{"--description=", "  Title  "}, "  Title  ", ""},
		{[]string{"Title", "--description", "текст с пробелами"}, "Title", "текст с пробелами"},
		{[]string{"--", "--title-is-literal"}, "--title-is-literal", ""},
		{[]string{"--description=--literal", "Title"}, "Title", "--literal"},
	} {
		parsed, err := parsePlanArgs("add", tc.args)
		if err != nil || parsed.title != tc.title || parsed.description != tc.description {
			t.Fatalf("%q: %+v %v", tc.args, parsed, err)
		}
	}
}
