package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestLifecycleSyntaxAndHelpBeforeDiscovery(t *testing.T) {
	for _, args := range [][]string{
		{"edit"}, {"pause"}, {"resume"}, {"archive"}, {"move", "--id=x"},
		{"edit", "--id=x", "--title="}, {"edit", "--id=x", "--title"},
		{"edit", "--id=x", "--description"}, {"edit", "--id=x", "--title=a", "--title=b"},
		{"move", "--id=x", "--after=y", "--end"}, {"move", "--id=x", "--before="},
		{"move", "--id=x", "--end=false"}, {"pause", "--id=x", "extra"},
		{"resume", "--id=x", "--id=x"}, {"archive", "--id=x", "--unknown"},
		{"pause", "--id="}, {"pause", "--id=x", "--", "--help"},
		{"edit", "--help", "--help"}, {"archive", "--help=true"},
	} {
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) {
			t.Fatal("discovered repository on invalid syntax")
			return nil, nil
		})
		if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("%v: %d %q %q", args, code, &out, &diagnostic)
		}
	}
	t.Setenv("PATH", "")
	for _, command := range []string{"edit", "move", "pause", "resume", "archive"} {
		for _, args := range [][]string{{"help", command}, {command, "--help"}, {command, "--help", "--"}} {
			var out, diagnostic bytes.Buffer
			if code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}); code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "Использование:") {
				t.Fatalf("help %v: %d %q %q", args, code, &out, &diagnostic)
			}
		}
	}
}

func TestLifecycleCLISequence(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	run := func(code int, args ...string) string {
		t.Helper()
		var out, diagnostic bytes.Buffer
		if actual := RunWithPlans(ctx, args, "test", Streams{Out: &out, Err: &diagnostic}, open); actual != code {
			t.Fatalf("%v: %d want %d: %q", args, actual, code, &diagnostic)
		}
		return out.String()
	}
	run(0, "edit", "--title= Точное имя 🙂 ", "--id=task-001", "--description=")
	run(0, "move", "--end", "--id=task-001")
	run(0, "start", "work", "--id=task-001")
	run(0, "pause", "--id=task-001")
	before := selectionBytes(t, c)
	if out := run(0, "pause", "--id=task-001"); !strings.Contains(out, "Изменений нет") || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatal("pause no-op was written")
	}
	testrepo.Run(t, c, "checkout", "main")
	run(0, "resume", "--id=task-001")
	run(1, "resume", "--id=task-001")
	run(0, "archive", "--id=task-001")
	before = selectionBytes(t, c)
	run(0, "archive", "--id=task-001")
	run(1, "resume", "--id=task-001")
	if !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatal("no-op or refused resume changed archive")
	}
	run(0, "attach", "work", "--id=task-002")
	var diagnostic bytes.Buffer
	if code := RunWithPlans(ctx, []string{"edit", "--id=task-001", "--description=Сохранено"}, "test", Streams{Out: failedWriter{}, Err: &diagnostic}, open); code != 1 || !strings.Contains(diagnostic.String(), "выполнена") {
		t.Fatalf("saved output failure: %d %q", code, &diagnostic)
	}
}

func TestEditorCancellationValidationAndConflict(t *testing.T) {
	for _, mode := range []string{"unchanged", "success", "cancel", "launch", "invalid", "empty title", "conflict", "context"} {
		t.Run(mode, func(t *testing.T) {
			p, c := selectionFixture(t)
			t.Setenv("GIT_EDITOR", "configured-editor --wait")
			ctx := context.Background()
			before := selectionBytes(t, c)
			var out, diagnostic bytes.Buffer
			var documentPath string
			streams := Streams{Out: &out, Err: &diagnostic, RunEditor: func(ctx context.Context, command, path, dir string) error {
				if command != "configured-editor --wait" || dir != c.Dir {
					t.Fatalf("editor boundary: %q %q", command, dir)
				}
				documentPath = path
				if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "write.lock")); !os.IsNotExist(err) {
					t.Fatalf("editor held lock: %v", err)
				}
				switch mode {
				case "unchanged":
					return nil
				case "cancel", "context":
					return context.Canceled
				case "launch":
					return errors.New("cannot start editor")
				case "invalid":
					return os.WriteFile(path, []byte(`{"title":"x","description":"", "id":"bad"}`), 0600)
				case "empty title":
					return os.WriteFile(path, []byte(`{"title":" ","description":""}`), 0600)
				case "conflict":
					if _, _, err := p.Move(ctx, "task-001", task.Position{End: true}); err != nil {
						return err
					}
					before = selectionBytes(t, c)
				}
				return os.WriteFile(path, []byte(`{"title":"Правка 🙂","description":""}`), 0600)
			}}
			open := func(context.Context) (*app.Plans, error) { return p, nil }
			code := RunWithPlans(ctx, []string{"edit", "--id=task-001"}, "test", streams, open)
			want := 1
			if mode == "success" || mode == "unchanged" {
				want = 0
			}
			if mode == "cancel" || mode == "context" {
				want = 130
			}
			if code != want {
				t.Fatalf("editor %s: code=%d want=%d: %q", mode, code, want, &diagnostic)
			}
			if mode != "success" && !bytes.Equal(before, selectionBytes(t, c)) {
				t.Fatal("failed/no-op edit changed plan")
			}
			if mode == "success" {
				var plan task.Plan
				if err := json.Unmarshal(selectionBytes(t, c), &plan); err != nil {
					t.Fatal(err)
				}
				item, _ := plan.FindID("task-001")
				if item.Title != "Правка 🙂" || item.Status != task.Todo {
					t.Fatalf("editor changed other fields: %+v", item)
				}
			}
			preserved := mode == "conflict" || mode == "invalid" || mode == "empty title"
			_, err := os.Stat(documentPath)
			if preserved {
				if err != nil || !strings.Contains(diagnostic.String(), documentPath) {
					t.Fatalf("lost rejected document: %v %q", err, &diagnostic)
				}
				if err := os.Remove(documentPath); err != nil {
					t.Fatal(err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("temporary document leaked: %v", err)
			}
		})
	}
}

func TestEditDocumentAndEditorArguments(t *testing.T) {
	for _, data := range []string{"null", `{}`, `{"title":null,"description":""}`, `{"title":"x"}`, `{"title":"x","description":1}`, `{"title":"x","description":""} {}`, `{"title":"x","title":"y","description":""}`, "\xff"} {
		if _, err := parseEditDocument([]byte(data)); err == nil {
			t.Fatalf("accepted invalid document: %q", data)
		}
	}
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{`code --wait`, []string{"code", "--wait"}},
		{`"C:\Program Files\Editor\editor.exe" --wait`, []string{`C:\Program Files\Editor\editor.exe`, "--wait"}},
		{`C:\Tools\editor.exe "a b" ''`, []string{`C:\Tools\editor.exe`, "a b", ""}},
		{`'/path with spaces/editor' --arg="$HOME;$(cmd)"`, []string{"/path with spaces/editor", "--arg=$HOME;$(cmd)"}},
		{`editor a\ b`, []string{"editor", "a b"}},
	} {
		got, err := splitEditor(tc.command)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %q %v", tc.command, got, err)
		}
	}
	for _, command := range []string{"", "  ", `"" arg`, `editor "bad`, "editor\x00"} {
		if _, err := splitEditor(command); err == nil {
			t.Fatalf("accepted invalid editor: %q", command)
		}
	}
}
