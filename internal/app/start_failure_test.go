package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestStartPostCheckoutFailureRecordsActualEffect(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	markDone(t, p)
	hooks := strings.TrimSpace(string(testrepo.Run(t, c, "config", "--get", "core.hooksPath")))
	// This hook also proves the child-only recursion marker is present.
	hook := []byte("#!/bin/sh\ntest -n \"$GIT_TASK_OPERATION\" || exit 19\nexit 23\n")
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), hook, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := p.Start(ctx, StartOptions{Branch: "hook-error", ID: "task-002", Again: true})
	if err == nil || !strings.Contains(err.Error(), "уже запущена") {
		t.Fatalf("start: %v", err)
	}
	if got.Status != task.Active || len(got.Attempts) != 1 {
		t.Fatalf("actual effect not saved: %+v", got)
	}
	if _, err = os.Stat(filepath.Join(c.Dir, ".git-task", "operation.json")); !os.IsNotExist(err) {
		t.Fatalf("journal: %v", err)
	}
}

func TestStartGitRefusalAndRacingBranch(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"ref-lock", "foreign-branch", "target-change", "external-plan"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			markDone(t, p)
			before := planBytes(t, c)
			p.checkpoint = func(point string) error {
				if point != "prepared" {
					return nil
				}
				switch mode {
				case "ref-lock":
					repo, _ := c.Discover(ctx)
					return os.WriteFile(filepath.Join(repo.GitDir, "refs", "heads", "race.lock"), []byte("foreign lock"), 0600)
				case "foreign-branch":
					testrepo.Run(t, c, "branch", "race")
				case "target-change":
					testrepo.Commit(t, c)
				case "external-plan":
					return os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), append(before, '\n'), 0600)
				}
				return nil
			}
			if _, err := p.Start(ctx, StartOptions{Branch: "race", ID: "task-002", Again: true}); err == nil {
				t.Fatal("race accepted")
			}
			expected := before
			if mode == "external-plan" {
				expected = append(append([]byte{}, before...), '\n')
			}
			if !bytes.Equal(expected, planBytes(t, c)) {
				t.Fatal("race overwrote plan")
			}
			p.checkpoint = nil
			if mode == "foreign-branch" || mode == "external-plan" {
				if _, err := p.RecoverStart(ctx); err == nil {
					t.Fatal("recovery accepted foreign state")
				}
			}
			if mode == "ref-lock" {
				if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "operation.json")); !os.IsNotExist(err) {
					t.Fatal("no effect retained journal", err)
				}
			}
		})
	}
}

func TestStartDestinationProtectsData(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"storage", "ignored-conflict"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			// Construct destination content in a separate index using real Git, without
			// ever overwriting the live plan or switching into a tracked metadata tree.
			alternate := *c
			alternate.Env = append(append([]string{}, c.Env...), "GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"))
			testrepo.Run(t, &alternate, "read-tree", "main")
			path := ".git-task/plan.json"
			payload := filepath.Join(c.Dir, ".git-task", "plan.json")
			if mode == "ignored-conflict" {
				path = "ignored.txt"
				payload = filepath.Join(c.Dir, path)
				if err := os.WriteFile(payload, []byte("user data"), 0600); err != nil {
					t.Fatal(err)
				}
				repo, _ := c.Discover(ctx)
				f, err := os.OpenFile(repo.ExcludePath, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("\nignored.txt\n")
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
			oid := strings.TrimSpace(string(testrepo.Run(t, c, "hash-object", "-w", "--", payload)))
			testrepo.Run(t, &alternate, "update-index", "--add", "--cacheinfo", "100644,"+oid+","+path)
			tree := strings.TrimSpace(string(testrepo.Run(t, &alternate, "write-tree")))
			commit := strings.TrimSpace(string(testrepo.Run(t, c, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit-tree", tree, "-p", "main", "-m", "destination")))
			before := planBytes(t, c)
			file, _ := os.ReadFile(payload)
			_, err := p.Start(ctx, StartOptions{Branch: "protected", From: commit})
			if err == nil {
				t.Fatal("unsafe destination accepted")
			}
			if !bytes.Equal(before, planBytes(t, c)) {
				t.Fatal("plan lost")
			}
			after, _ := os.ReadFile(payload)
			if !bytes.Equal(file, after) {
				t.Fatal("ignored user file lost")
			}
			if mode == "storage" {
				branch, _ := c.BranchCommit(ctx, "protected")
				if branch != "" {
					t.Fatal("created branch before tree check")
				}
			}
		})
	}
}

func TestRecoveryRejectsChangesAndPreservesCompletedWrite(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"source-edit", "result-edit", "journal-edit", "branch-recreated", "repair-interrupted", "new"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			stop := errors.New("stop")
			boundary := "checked-out"
			if mode == "result-edit" {
				boundary = "committed"
			}
			p.checkpoint = func(point string) error {
				if point == boundary {
					return stop
				}
				return nil
			}
			options := StartOptions{Branch: "recovery", ID: "task-001"}
			if mode == "new" {
				options.ID = ""
				options.New = "Новая"
			}
			if _, err := p.Start(ctx, options); !errors.Is(err, stop) {
				t.Fatal(err)
			}
			p.checkpoint = nil
			planPath := filepath.Join(c.Dir, ".git-task", "plan.json")
			journalPath := filepath.Join(c.Dir, ".git-task", "operation.json")
			switch mode {
			case "source-edit", "result-edit":
				data := planBytes(t, c)
				if err := os.WriteFile(planPath, append(data, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			case "journal-edit":
				data, err := os.ReadFile(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`"branch": "recovery"`), []byte(`"branch": "altered"`), 1)
				if err = os.WriteFile(journalPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "branch-recreated":
				testrepo.Run(t, c, "checkout", "main")
				testrepo.Run(t, c, "branch", "-D", "recovery")
				testrepo.Run(t, c, "checkout", "-b", "recovery")
			case "repair-interrupted":
				p.checkpoint = func(point string) error {
					if point == "recovered" {
						return stop
					}
					return nil
				}
			}
			before := planBytes(t, c)
			_, err := p.RecoverStart(ctx)
			if mode == "new" {
				if err != nil {
					t.Fatal(err)
				}
				plan, _ := p.Status(ctx)
				if len(plan.Tasks) != 4 {
					t.Fatal("new task recovery lost/duplicated task")
				}
				return
			}
			if err == nil {
				t.Fatal("recovery accepted altered state")
			}
			if mode != "repair-interrupted" && !bytes.Equal(before, planBytes(t, c)) {
				t.Fatal("recovery overwrote external state")
			}
			if mode == "repair-interrupted" {
				saved := planBytes(t, c)
				backup, _ := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				p.checkpoint = nil
				if _, err = p.RecoverStart(ctx); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(saved, planBytes(t, c)) {
					t.Fatal("second repair changed result")
				}
				after, _ := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				if !bytes.Equal(backup, after) {
					t.Fatal("second repair changed backup")
				}
			}
		})
	}
}

func TestStartExcludesConcurrentWriters(t *testing.T) {
	t.Parallel()
	p, _ := startFixture(t)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	p.checkpoint = func(point string) error {
		if point == "prepared" {
			close(entered)
			<-release
		}
		return nil
	}
	finished := make(chan error, 1)
	go func() { _, err := p.Start(ctx, StartOptions{Branch: "serialized"}); finished <- err }()
	<-entered
	_, err := p.Add(ctx, "concurrent", "", task.Position{})
	close(release)
	if startErr := <-finished; startErr != nil {
		t.Fatal(startErr)
	}
	if !errors.Is(err, storage.ErrLocked) {
		t.Fatalf("concurrent writer: %v", err)
	}
}

func TestStartRefChangeBeforeCheckout(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	initial, _ := c.HeadState(ctx)
	testrepo.Run(t, c, "checkout", "-b", "other")
	testrepo.Commit(t, c)
	other, _ := c.BranchCommit(ctx, "other")
	testrepo.Run(t, c, "checkout", "main")
	p.checkpoint = func(point string) error {
		if point == "created" {
			testrepo.Run(t, c, "update-ref", "refs/heads/replaced", other)
		}
		return nil
	}
	before := planBytes(t, c)
	if _, err := p.Start(ctx, StartOptions{Branch: "replaced"}); err == nil {
		t.Fatal("foreign ref accepted")
	}
	after, _ := c.HeadState(ctx)
	if after != initial || !bytes.Equal(before, planBytes(t, c)) {
		t.Fatal("switched to replaced ref or saved plan")
	}
}
