package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func transferFixture(t *testing.T) (*app.Plans, *git.Client) {
	t.Helper()
	c := testrepo.New(t)
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
	p, err := app.Open(context.Background(), c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = p.Init(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	return p, c
}

func transferSource(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "план с пробелами.md")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTransferArgumentsAndHelpWithoutRepository(t *testing.T) {
	for _, args := range [][]string{
		{"import"}, {"import", "one", "two"}, {"export", "extra"}, {"import", " "},
		{"import", "x", "--format=yaml"}, {"export", "--format=JSON"}, {"export", "--format"},
		{"export", "--output"}, {"export", "--output="}, {"export", "--yes"}, {"import", "x", "--output", "out"},
		{"import", "x", "--yes=false"}, {"import", "x", "--yes", "--yes"}, {"export", "--format=json", "--format=markdown"},
		{"import", "x", "-y"}, {"export", "--help", "--help"}, {"export", "--output=a", "--output=b"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) {
				t.Fatal("opened repository before validation")
				return nil, nil
			})
			if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
				t.Fatalf("code=%d out=%q err=%q", code, &out, &diagnostic)
			}
		})
	}
	t.Setenv("PATH", "")
	for _, args := range [][]string{{"help", "import"}, {"help", "export"}, {"import", "--help"}, {"export", "--help"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}); code != 0 || !strings.Contains(out.String(), "Использование:") || diagnostic.Len() != 0 {
			t.Fatalf("help %v: %d %q %q", args, code, &out, &diagnostic)
		}
	}
	if parsed, err := parseTransferArgs("import", []string{"--format=json", "--yes", "--", "--source"}); err != nil || parsed.path != "--source" || !parsed.yes || parsed.format != "json" {
		t.Fatalf("option boundary: %+v %v", parsed, err)
	}
}

func TestImportConfirmationPreviewAndCancellation(t *testing.T) {
	for _, mode := range []string{"yes", "interactive", "no", "default no", "EOF", "nonterminal", "context", "bad input", "preview failure", "changed plan"} {
		t.Run(mode, func(t *testing.T) {
			p, c := transferFixture(t)
			input := "да\n"
			data := "- [ ] Unicode 🙂\n- [x] Готово\n"
			args := []string{"import"}
			switch mode {
			case "no":
				input = "нет\n"
			case "default no":
				input = "\n"
			case "EOF":
				input = ""
			case "bad input":
				data += "  - [ ] Вложенный\n"
			}
			args = append(args, transferSource(t, data))
			if mode == "yes" {
				args = append(args, "--yes")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			before := selectionBytes(t, c)
			dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader(input))}
			if mode == "context" {
				dialogue.before = cancel
			}
			if mode == "changed plan" {
				dialogue.before = func() {
					if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), append(before, '\n'), 0600); err != nil {
						t.Fatal(err)
					}
					before = selectionBytes(t, c)
				}
			}
			var out, diagnostic bytes.Buffer
			streams := Streams{Out: &out, Err: &diagnostic, OpenDialogue: func() (Dialogue, error) {
				if mode == "yes" || mode == "bad input" || mode == "preview failure" {
					t.Fatal("opened unnecessary dialogue")
				}
				return dialogue, nil
			}}
			if mode == "nonterminal" {
				streams.OpenDialogue = nil
			}
			if mode == "preview failure" {
				streams.Err = failedWriter{}
			}
			code := RunWithPlans(ctx, args, "test", streams, func(context.Context) (*app.Plans, error) { return p, nil })
			if mode == "yes" || mode == "interactive" {
				if code != 0 || !strings.Contains(out.String(), "Импортировано задач: 2") || !strings.Contains(diagnostic.String(), "Задач: 2") || !strings.Contains(diagnostic.String(), "done") || !strings.Contains(diagnostic.String(), "Назначение:") {
					t.Fatalf("success: %d %q %q", code, &out, &diagnostic)
				}
			} else {
				if code == 0 || out.Len() != 0 || !bytes.Equal(before, selectionBytes(t, c)) {
					t.Fatalf("cancel/error changed plan: %d %q %q", code, &out, &diagnostic)
				}
				if (mode == "no" || mode == "default no" || mode == "EOF" || mode == "context") && code != 130 {
					t.Fatalf("cancellation code=%d", code)
				}
			}
			if mode == "interactive" || mode == "no" || mode == "default no" || mode == "EOF" || mode == "context" || mode == "changed plan" {
				if !dialogue.closed {
					t.Fatal("dialogue leaked")
				}
			}
		})
	}
}

func TestTransferJSONSettingsAndExportStreams(t *testing.T) {
	p, c := transferFixture(t)
	source := transferSource(t, `{"format":"git-task","schema_version":1,"revision":0,"target_branch":"release","order":[],"tasks":[],"last_event":0}`)
	var out, diagnostic bytes.Buffer
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	code := RunWithPlans(context.Background(), []string{"import", source, "--format", "json", "--yes"}, "test", Streams{Out: &out, Err: &diagnostic}, open)
	if code != 0 || !strings.Contains(diagnostic.String(), `target_branch="release"`) {
		t.Fatalf("settings preview: %d %q", code, &diagnostic)
	}
	if _, err := p.Add(context.Background(), "Одна", "Описание", task.Position{}); err != nil {
		t.Fatal(err)
	}
	before := selectionBytes(t, c)
	for _, format := range []string{"markdown", "json"} {
		out.Reset()
		diagnostic.Reset()
		code = RunWithPlans(context.Background(), []string{"export", "--format", format}, "test", Streams{Out: &out, Err: &diagnostic}, open)
		if code != 0 || !bytes.Equal(before, selectionBytes(t, c)) {
			t.Fatalf("export: %d %q", code, &diagnostic)
		}
		if format == "markdown" && (out.String() != "- [ ] Одна\n" || !strings.Contains(diagnostic.String(), "история")) {
			t.Fatalf("markdown: %q %q", &out, &diagnostic)
		}
		if format == "json" && (!strings.HasPrefix(out.String(), "{") || diagnostic.Len() != 0) {
			t.Fatalf("JSON contamination: %q %q", &out, &diagnostic)
		}
	}
	output := filepath.Join(c.Dir, "export.json")
	out.Reset()
	diagnostic.Reset()
	if code = RunWithPlans(context.Background(), []string{"export", "--output", output, "--format=json"}, "test", Streams{Out: &out, Err: &diagnostic}, open); code != 0 || out.Len() != 0 {
		t.Fatalf("file export: %d %q %q", code, &out, &diagnostic)
	}
	for _, streams := range []Streams{{Out: failedWriter{}, Err: &diagnostic}, {Out: &out, Err: failedWriter{}}} {
		if code = RunWithPlans(context.Background(), []string{"export"}, "test", streams, open); code != 1 {
			t.Fatalf("write error code=%d", code)
		}
	}
}

func TestImportResultWriteFailureReportsSavedState(t *testing.T) {
	p, _ := transferFixture(t)
	var diagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"import", transferSource(t, "- [ ] One"), "--yes"}, "test", Streams{Out: failedWriter{}, Err: &diagnostic}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 1 || !strings.Contains(diagnostic.String(), "импорт уже сохранён") {
		t.Fatalf("lost write context: %d %q", code, &diagnostic)
	}
	plan, err := p.Status(context.Background())
	if err != nil || len(plan.Tasks) != 1 {
		t.Fatalf("saved state lost: %+v %v", plan, err)
	}
}

func TestImportRechecksTrackedStorageAfterConfirmation(t *testing.T) {
	p, c := transferFixture(t)
	before := selectionBytes(t, c)
	dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader("да\n")), before: func() {
		if _, err := c.Run(context.Background(), "add", "-f", "--", ".git-task/plan.json"); err != nil {
			t.Fatal(err)
		}
	}}
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"import", transferSource(t, "- [ ] One")}, "test", Streams{Out: &out, Err: &diagnostic, OpenDialogue: func() (Dialogue, error) { return dialogue, nil }}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 1 || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("tracked storage overwritten: %d %q", code, &diagnostic)
	}
	if _, err := p.Status(context.Background()); err == nil || errors.Is(err, storage.ErrConflict) {
		t.Fatalf("tracked path not rejected: %v", err)
	}
}
