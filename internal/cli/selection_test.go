package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-task/internal/app"
	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

type scriptedDialogue struct {
	reader *bufio.Reader
	before func()
	closed bool
}

func (d *scriptedDialogue) ReadLine(ctx context.Context) (string, error) {
	if d.before != nil {
		before := d.before
		d.before = nil
		before()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return d.reader.ReadString('\n')
}

func (d *scriptedDialogue) Close() error { d.closed = true; return nil }

func selectionFixture(t *testing.T) (*app.Plans, *git.Client) {
	t.Helper()
	c := testrepo.New(t)
	testrepo.Commit(t, c)
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
	if _, _, err := p.Init(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := storage.New(c.Dir, repo.ExcludePath, c)
	snapshot, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan := snapshot.Plan
	for _, title := range []string{"Первая", "Одинаковая", "Одинаковая"} {
		if _, err := plan.Add(title, "", task.Position{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Save(context.Background(), snapshot, plan); err != nil {
		t.Fatal(err)
	}
	return p, c
}

func selectionRun(ctx context.Context, p *app.Plans, input string, before func(), args ...string) (int, string, string, bool) {
	var out, diagnostic bytes.Buffer
	dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader(input)), before: before}
	code := RunWithPlans(ctx, append([]string{"start", "feature", "--select"}, args...), "test", Streams{
		Out: &out, Err: &diagnostic,
		OpenDialogue: func() (Dialogue, error) { return dialogue, nil },
	}, func(context.Context) (*app.Plans, error) { return p, nil })
	return code, out.String(), diagnostic.String(), dialogue.closed
}

func selectionBytes(t *testing.T, c *git.Client) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func selectionUnchanged(t *testing.T, c *git.Client, data, refs, head []byte) {
	t.Helper()
	if !bytes.Equal(data, selectionBytes(t, c)) || !bytes.Equal(refs, testrepo.Run(t, c, "show-ref")) || !bytes.Equal(head, testrepo.Run(t, c, "rev-parse", "HEAD")) {
		t.Fatal("selection changed plan, refs or HEAD")
	}
	for _, name := range []string{"operation.json", "write.lock"} {
		if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestSelectExistingAndNewUseStart(t *testing.T) {
	for _, tc := range []struct{ name, input, id, title, from string }{
		{"existing", "2\n", "task-002", "Одинаковая", ""},
		{"invalid_then_existing", "999\n-1\nabc\n\n999999999999999999999\n3\n", "task-003", "Одинаковая", ""},
		{"new", "n\n\n   \nНовая 🐱\n", "task-004", "Новая 🐱", ""},
		{"explicit_base", "1\n", "task-001", "Первая", "side"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, c := selectionFixture(t)
			testrepo.Run(t, c, "checkout", "-b", "side")
			testrepo.Commit(t, c)
			baseName := "main"
			var args []string
			if tc.from != "" {
				baseName = tc.from
				args = []string{"--from", tc.from}
			}
			base := strings.TrimSpace(string(testrepo.Run(t, c, "rev-parse", baseName)))
			code, out, diagnostic, closed := selectionRun(context.Background(), p, tc.input, nil, args...)
			if code != 0 || !closed || !strings.Contains(out, tc.id) || !strings.Contains(diagnostic, "Основание: "+baseName+" (") {
				t.Fatalf("code=%d closed=%v out=%q diagnostic=%q", code, closed, out, diagnostic)
			}
			plan, err := p.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			selected, err := plan.FindID(tc.id)
			if err != nil || selected.Title != tc.title || selected.Status != task.Active || selected.ActiveAttempt == nil || len(selected.Attempts) != 0 || selected.ActiveAttempt.BaseCommit != base || selected.ActiveAttempt.TargetBranch != "main" {
				t.Fatalf("task=%+v err=%v", selected, err)
			}
			if strings.TrimSpace(string(testrepo.Run(t, c, "rev-parse", "HEAD"))) != base || strings.TrimSpace(string(testrepo.Run(t, c, "symbolic-ref", "--short", "HEAD"))) != "feature" {
				t.Fatal("wrong Git branch/base")
			}
			if bytes.Contains(selectionBytes(t, c), []byte(`"attempts"`)) {
				t.Fatal("unfinished attempt in history")
			}
		})
	}
}

func TestSelectCancellationPreservesState(t *testing.T) {
	for _, input := range []string{"0\n", "q\n", "", "2", "n\n", "n\nИмя", "n\nq\n", "n\n0\n", "\x03\n", "n\n\x04\n", "\x1a\n"} {
		t.Run(input, func(t *testing.T) {
			p, c := selectionFixture(t)
			data, refs, head := selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
			code, out, diagnostic, closed := selectionRun(context.Background(), p, input, nil)
			if code != 130 || out != "" || !closed || !strings.Contains(diagnostic, "отменена") {
				t.Fatalf("code=%d closed=%v out=%q diagnostic=%q", code, closed, out, diagnostic)
			}
			selectionUnchanged(t, c, data, refs, head)
		})
	}
}

func TestSelectContextCancellationAfterPrompt(t *testing.T) {
	p, c := selectionFixture(t)
	data, refs, head := selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	code, out, _, closed := selectionRun(ctx, p, "1\n", cancel)
	if code != 130 || out != "" || !closed {
		t.Fatalf("code=%d closed=%v out=%q", code, closed, out)
	}
	selectionUnchanged(t, c, data, refs, head)
}

func TestSelectConflictsAfterWaiting(t *testing.T) {
	for _, change := range []string{"process_add", "whitespace", "title", "status", "reorder", "base", "head", "occupied"} {
		t.Run(change, func(t *testing.T) {
			p, c := selectionFixture(t)
			var data, refs, head []byte
			before := func() {
				if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "write.lock")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("lock held during input: %v", err)
				}
				switch change {
				case "process_add":
					executable, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()
					cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSelectionWriterProcess$")
					cmd.Dir, cmd.Env = c.Dir, append(c.Env, "GIT_TASK_SELECTION_WRITER=1")
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("writer: %v %s", err, output)
					}
				case "base":
					testrepo.Commit(t, c)
				case "head":
					testrepo.Run(t, c, "checkout", "-b", "elsewhere")
				case "occupied":
					testrepo.Run(t, c, "branch", "feature")
				default:
					data := selectionBytes(t, c)
					if change == "whitespace" {
						data = append(data, '\n')
					} else {
						var plan task.Plan
						if err := json.Unmarshal(data, &plan); err != nil {
							t.Fatal(err)
						}
						switch change {
						case "title":
							plan.Tasks[0].Title = "Изменена"
						case "status":
							if _, err := plan.Archive("task-001"); err != nil {
								t.Fatal(err)
							}
						case "reorder":
							if _, err := plan.Move("task-001", task.Position{End: true}); err != nil {
								t.Fatal(err)
							}
						}
						var err error
						data, err = json.Marshal(plan)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				data, refs, head = selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
			}
			code, out, diagnostic, _ := selectionRun(context.Background(), p, "1\n", before)
			if code != 1 || out != "" || diagnostic == "" {
				t.Fatalf("code=%d out=%q diagnostic=%q", code, out, diagnostic)
			}
			selectionUnchanged(t, c, data, refs, head)
		})
	}
}

func TestSelectionWriterProcess(t *testing.T) {
	if os.Getenv("GIT_TASK_SELECTION_WRITER") != "1" {
		return
	}
	p, err := app.Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Add(context.Background(), "Другой процесс", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
}

func TestSelectShowsUnavailableStates(t *testing.T) {
	p, c := selectionFixture(t)
	data := selectionBytes(t, c)
	var plan task.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, item := range []struct{ id, branch string }{{"task-001", "active"}, {"task-002", "paused"}} {
		if _, err := plan.Start(item.id, task.Attempt{ID: item.id, Branch: item.branch, OriginalBranch: item.branch, TargetBranch: "main", BaseCommit: strings.TrimSpace(string(testrepo.Run(t, c, "rev-parse", "HEAD"))), StartedAt: &now}, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := plan.Pause("task-002"); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Complete("task-003", "done", task.Completion{Source: task.Imported, TargetBranch: "main", ObservedAt: &now}); err != nil {
		t.Fatal(err)
	}
	archived, err := plan.Add("Архив", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Archive(archived.ID); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	refs, head := testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	code, _, diagnostic, _ := selectionRun(context.Background(), p, "1\n0\n", nil)
	if code != 130 {
		t.Fatalf("code=%d %s", code, diagnostic)
	}
	for _, want := range []string{"active", "paused", "done", "archived", "уже начата", "resume", "--again", "в архиве", "Неверный выбор"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("missing %q: %s", want, diagnostic)
		}
	}
	selectionUnchanged(t, c, data, refs, head)
}

func TestSelectNonTerminalAndAutomaticRegression(t *testing.T) {
	p, c := selectionFixture(t)
	data, refs, head := selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	var out, diagnostic bytes.Buffer
	open := func(context.Context) (*app.Plans, error) { return p, nil }
	streams := Streams{In: strings.NewReader("1\n"), Out: &out, Err: &diagnostic}
	if code := RunWithPlans(context.Background(), []string{"start", "feature", "--select"}, "test", streams, open); code != 1 || !strings.Contains(diagnostic.String(), "терминал") {
		t.Fatalf("nonterminal: %d %s", code, &diagnostic)
	}
	selectionUnchanged(t, c, data, refs, head)
	diagnostic.Reset()
	streams.OpenDialogue = func() (Dialogue, error) { t.Fatal("automatic start opened menu"); return nil, nil }
	if code := RunWithPlans(context.Background(), []string{"start", "feature"}, "test", streams, open); code != 0 || !strings.Contains(out.String(), "task-001") || diagnostic.Len() != 0 {
		t.Fatalf("automatic: %d %s %s", code, &out, &diagnostic)
	}
}

func TestSelectNewTaskGitFailurePreservesState(t *testing.T) {
	p, c := selectionFixture(t)
	path := filepath.Join(c.Dir, "tracked")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "add", "tracked")
	testrepo.Commit(t, c)
	if err := os.WriteFile(path, []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	data, refs, head := selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	code, out, diagnostic, _ := selectionRun(context.Background(), p, "n\nНовая\n", nil)
	if code != 1 || out != "" || diagnostic == "" {
		t.Fatalf("code=%d out=%q diagnostic=%q", code, out, diagnostic)
	}
	selectionUnchanged(t, c, data, refs, head)
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "dirty" {
		t.Fatalf("working file=%q err=%v", contents, err)
	}
}

func TestSelectNewTaskRefusesChangedPlan(t *testing.T) {
	p, c := selectionFixture(t)
	var data, refs, head []byte
	before := func() {
		if _, err := p.Add(context.Background(), "Чужая", "", task.Position{}); err != nil {
			t.Fatal(err)
		}
		data, refs, head = selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	}
	code, out, diagnostic, _ := selectionRun(context.Background(), p, "n\nНовая\n", before)
	if code != 1 || out != "" || !strings.Contains(diagnostic, "повторите --select") {
		t.Fatalf("code=%d out=%q diagnostic=%q", code, out, diagnostic)
	}
	selectionUnchanged(t, c, data, refs, head)
}
