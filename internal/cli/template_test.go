package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestInitTemplateCLIWorkflow(t *testing.T) {
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
	if code, _, diagnostic := prefixCLI(p, "init", "--apply"); code != 1 || !strings.Contains(diagnostic, "сначала выполните git task init") {
		t.Fatalf("missing template: %d %q", code, diagnostic)
	}
	if code, out, diagnostic := prefixCLI(p, "init"); code != 0 || !strings.Contains(out, "git task init --apply") || diagnostic != "" {
		t.Fatalf("create template: %d %q %q", code, out, diagnostic)
	}
	path := filepath.Join(c.Dir, ".git-task", "plan.json")
	before := selectionBytes(t, c)
	refs, head := testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	for _, args := range [][]string{{"init", "--apply"}, {"add", "Rejected"}, {"start", "feature/rejected"}, {"show", "--id", "missing"}, {"status"}} {
		if code, _, diagnostic := prefixCLI(p, args...); code != 1 || diagnostic == "" {
			t.Fatalf("unfinished template %v: %d %q", args, code, diagnostic)
		}
		selectionUnchanged(t, c, before, refs, head)
	}
	filled := []byte(`{"target_branch":"main","tasks":[{"title":"First"},{"title":"Second"}]}`)
	if err := os.WriteFile(path, filled, 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := prefixCLI(p, "init"); code != 0 || diagnostic != "" || !bytes.Equal(filled, selectionBytes(t, c)) {
		t.Fatalf("init applied draft implicitly: %d %q", code, diagnostic)
	}
	if code, out, diagnostic := prefixCLI(p, "init", "--apply"); code != 0 || !strings.Contains(out, "Шаблон применён") || diagnostic != "" {
		t.Fatalf("apply: %d %q %q", code, out, diagnostic)
	}
	var plan task.Plan
	if err := json.Unmarshal(selectionBytes(t, c), &plan); err != nil || len(plan.Tasks) != 2 {
		t.Fatalf("ready plan: %+v %v", plan, err)
	}
	if code, _, diagnostic := prefixCLI(p, "start", "feature/first"); code != 0 || diagnostic != "" {
		t.Fatalf("start: %d %q", code, diagnostic)
	}
	if err := json.Unmarshal(selectionBytes(t, c), &plan); err != nil || plan.Tasks[0].Status != task.Active || len(plan.Tasks[0].Attempts) != 1 || plan.Tasks[1].Status != task.Todo {
		t.Fatalf("start did not choose first template task: %+v %v", plan, err)
	}
	before = selectionBytes(t, c)
	if code, out, diagnostic := prefixCLI(p, "init", "--apply"); code != 0 || !strings.Contains(out, "уже инициализирован") || diagnostic != "" || !bytes.Equal(before, selectionBytes(t, c)) {
		t.Fatalf("reapply changed history: %d %q %q", code, out, diagnostic)
	}
}
