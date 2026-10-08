package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func startFixture(t *testing.T) (*Plans, *git.Client) {
	t.Helper()
	c := testrepo.New(t)
	testrepo.Commit(t, c)
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := &Plans{git: c, store: storage.New(repo.Root, repo.ExcludePath, c)}
	if _, _, err = p.Init(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	// These tests exercise start, not three repetitions of Add's Git guards.
	// Prepare the same validated fixture with a single storage transaction.
	editPlan(t, p, func(plan *task.Plan) error {
		for _, title := range []string{"Первая", " Добавить профиль ", "Третья"} {
			if _, err := plan.Add(title, "", task.Position{}); err != nil {
				return err
			}
		}
		testrepo.FixtureIDs(plan)
		return nil
	})
	return p, c
}

func planBytes(t *testing.T, c *git.Client) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func editPlan(t *testing.T, p *Plans, edit func(*task.Plan) error) {
	t.Helper()
	ctx := context.Background()
	s, err := p.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := s.Plan
	if err = edit(&next); err != nil {
		t.Fatal(err)
	}
	testrepo.FixtureIDs(&next)
	if _, err = p.store.Save(ctx, s, next); err != nil {
		t.Fatal(err)
	}
}

func markDone(t *testing.T, p *Plans) {
	t.Helper()
	editPlan(t, p, func(plan *task.Plan) error {
		_, err := plan.CompleteManual("task-002", "prior-attempt", "", time.Now().UTC())
		return err
	})
}

func TestStartSelectionAndTarget(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	editPlan(t, p, func(plan *task.Plan) error {
		_, err := plan.CompleteManual("task-001", "prior-attempt", "", time.Now().UTC())
		return err
	})
	// Reorder so ID and display position cannot be mistaken for one another.
	editPlan(t, p, func(plan *task.Plan) error {
		_, err := plan.Move("task-003", task.Position{Before: "task-001"})
		return err
	})
	target, _ := c.BranchCommit(ctx, "main")
	third, err := p.Start(ctx, StartOptions{Branch: "feature-three", ID: "task-003"})
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Commit(t, c)
	first, err := p.Start(ctx, StartOptions{Branch: "feature-one"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "task-002" || third.ID != "task-003" || first.ActiveAttempt.BaseCommit != target || third.ActiveAttempt.BaseCommit != target {
		t.Fatalf("incorrect selection/base: %+v %+v", first, third)
	}
	if bytes.Contains(planBytes(t, c), []byte(`"attempts": []`)) {
		t.Fatal("empty attempts serialized")
	}
	if got, _ := c.BranchCommit(ctx, "feature-one"); got != target {
		t.Fatalf("base=%s want=%s", got, target)
	}
	editPlan(t, p, func(plan *task.Plan) error {
		title := "Новое имя"
		_, err := plan.Edit(third.ID, task.EditOptions{Title: &title})
		return err
	})
	plan, err := p.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := plan.FindID(third.ID)
	if current.ActiveAttempt.ID != third.ActiveAttempt.ID {
		t.Fatal("title edit changed binding")
	}
	before := planBytes(t, c)
	if _, err = p.Start(ctx, StartOptions{Branch: "no-todo"}); err == nil {
		t.Fatal("no todo accepted")
	}
	if !bytes.Equal(before, planBytes(t, c)) {
		t.Fatal("no todo changed plan")
	}
}

func TestStartSelectorsAndAgain(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{"id", "title"} {
		for _, done := range []bool{false, true} {
			for _, again := range []bool{false, true} {
				t.Run(selector+fmtBool(done)+fmtBool(again), func(t *testing.T) {
					p, c := startFixture(t)
					ctx := context.Background()
					if done {
						markDone(t, p)
					}
					before := planBytes(t, c)
					previous, _ := p.Status(ctx)
					old, _ := previous.FindID("task-002")
					options := StartOptions{Branch: "profile", Again: again}
					if selector == "id" {
						options.ID = "task-002"
					} else {
						options.Title = " Добавить профиль "
					}
					result, err := p.Start(ctx, options)
					if done != again {
						if err == nil {
							t.Fatal("invalid transition accepted")
						}
						if done && !strings.Contains(err.Error(), "--"+selector) {
							t.Fatalf("missing selector hint: %v", err)
						}
						if !bytes.Equal(before, planBytes(t, c)) {
							t.Fatal("rejected start changed plan")
						}
						if branch, _ := c.BranchCommit(ctx, "profile"); branch != "" {
							t.Fatal("rejected start created branch")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if result.ID != "task-002" || result.Status != task.Active || len(old.Attempts) > 0 && !reflect.DeepEqual(old.Attempts, result.Attempts[:len(old.Attempts)]) {
						t.Fatalf("bad result: %+v", result)
					}
					if len(result.Attempts) != len(old.Attempts)+1 || result.ActiveAttempt.Completion != nil {
						t.Fatal("start lost or completed an approach")
					}
					after := planBytes(t, c)
					options.Branch = "second-attempt"
					second, secondErr := p.Start(ctx, options)
					if !done {
						if secondErr != nil || len(second.Attempts) != len(result.Attempts)+1 {
							t.Fatalf("parallel start: %+v %v", second, secondErr)
						}
						return
					}
					if secondErr == nil {
						t.Fatal("--again accepted for active")
					}
					if !bytes.Equal(after, planBytes(t, c)) {
						t.Fatal("repeat changed plan")
					}
				})
			}
		}
	}
}

func fmtBool(value bool) string {
	if value {
		return "-true"
	}
	return "-false"
}

// deadlineGuard checks the context at the storage boundary without sleeping or
// relying on the host being slow enough to exhaust the former five-second cap.
type deadlineGuard struct {
	*git.Client
	deadline time.Time
}

func (g deadlineGuard) CheckStorage(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(g.deadline) {
		return errors.New("normal start replaced the caller deadline")
	}
	return g.Client.CheckStorage(ctx)
}

func TestStartPreservesCallerDeadline(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.store = storage.New(repo.Root, repo.ExcludePath, deadlineGuard{Client: c, deadline: deadline})
	if _, err = p.Start(ctx, StartOptions{Branch: "caller-context"}); err != nil {
		t.Fatal(err)
	}
}

func TestStartInvalidSelectionPreservesState(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	editPlan(t, p, func(plan *task.Plan) error {
		item, err := plan.Add("Первая", "", task.Position{})
		if err != nil {
			return err
		}
		_, err = plan.Archive(item.ID)
		return err
	})
	before := planBytes(t, c)
	for _, options := range []StartOptions{
		{ID: "2"}, {ID: "unknown-task"}, {ID: "missing"}, {Title: "Первая"}, {Title: "Добавить профиль"}, {Title: " добавить профиль "}, {Title: "missing"},
		{ID: "task-001", Title: "Первая"}, {Again: true}, {New: "Новая", Again: true},
		{Branch: "--force"}, {Branch: "@{-1}"}, {Branch: "refs/heads/name"}, {Branch: "HEAD"}, {Branch: "main"},
	} {
		if options.Branch == "" {
			options.Branch = "rejected"
		}
		if _, err := p.Start(ctx, options); err == nil {
			t.Fatalf("accepted %+v", options)
		}
		if !bytes.Equal(before, planBytes(t, c)) {
			t.Fatalf("changed state %+v", options)
		}
	}
}

func TestStartFromAndNew(t *testing.T) {
	t.Parallel()
	p, c := startFixture(t)
	ctx := context.Background()
	testrepo.Run(t, c, "checkout", "-b", "other")
	testrepo.Commit(t, c)
	other, _ := c.BranchCommit(ctx, "other")
	got, err := p.Start(ctx, StartOptions{Branch: "new-task", New: "Новая", From: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveAttempt.BaseCommit != other || got.ActiveAttempt.TargetBranch != "main" {
		t.Fatalf("bad new task: %+v", got)
	}
	testrepo.Run(t, c, "tag", "ambiguous")
	testrepo.Run(t, c, "branch", "ambiguous")
	for _, from := range []string{"ambiguous", "HEAD~1", "other^{commit}", "--help", "missing"} {
		before := planBytes(t, c)
		if _, err = p.Start(ctx, StartOptions{Branch: "bad", New: "Не сохранить", From: from}); err == nil {
			t.Fatalf("accepted %s", from)
		}
		if !bytes.Equal(before, planBytes(t, c)) {
			t.Fatal("failed --new changed plan")
		}
	}
	for i, from := range []string{"refs/heads/other", other, "refs/tags/ambiguous"} {
		branch := []string{"full-ref", "commit-id", "tag"}[i]
		if _, err = p.Start(ctx, StartOptions{Branch: branch, New: branch, From: from}); err != nil {
			t.Fatal(err)
		}
	}
	testrepo.Run(t, c, "branch", "-D", "main")
	if _, err = p.Start(ctx, StartOptions{Branch: "missing-target", From: other}); err == nil {
		t.Fatal("missing target accepted")
	}
}

func TestStartDirtyAndUnfinished(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"untracked", "staged", "unstaged", "merge", "ignored"} {
		t.Run(kind, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			path := filepath.Join(c.Dir, "file.txt")
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			testrepo.Run(t, c, "add", "file.txt")
			testrepo.Commit(t, c)
			switch kind {
			case "untracked":
				path = filepath.Join(c.Dir, "new.txt")
				if err := os.WriteFile(path, []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			case "staged", "unstaged":
				if err := os.WriteFile(path, []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "staged" {
					testrepo.Run(t, c, "add", "file.txt")
				}
			case "merge":
				repo, _ := c.Discover(ctx)
				if err := os.WriteFile(filepath.Join(repo.GitDir, "MERGE_HEAD"), []byte("unfinished"), 0600); err != nil {
					t.Fatal(err)
				}
			case "ignored":
				repo, _ := c.Discover(ctx)
				f, err := os.OpenFile(repo.ExcludePath, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("\nignored.txt\n")
				if closeErr := f.Close(); err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				path = filepath.Join(c.Dir, "ignored.txt")
				if err = os.WriteFile(path, []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := planBytes(t, c)
			index := testrepo.Run(t, c, "ls-files", "--stage")
			file, _ := os.ReadFile(path)
			_, err := p.Start(ctx, StartOptions{Branch: "clean"})
			if (kind == "ignored") != (err == nil) {
				t.Fatalf("start %s: %v", kind, err)
			}
			if kind != "ignored" && !bytes.Equal(before, planBytes(t, c)) {
				t.Fatal("failure changed plan")
			}
			if !bytes.Equal(index, testrepo.Run(t, c, "ls-files", "--stage")) {
				t.Fatal("index changed")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(file, after) {
				t.Fatal("user file changed")
			}
		})
	}
}

func TestStartRecoveryBoundaries(t *testing.T) {
	t.Parallel()
	for _, done := range []bool{false, true} {
		for _, point := range []string{"prepared", "created", "checked-out", "committed"} {
			t.Run(point+fmtBool(done), func(t *testing.T) {
				p, c := startFixture(t)
				ctx := context.Background()
				if done {
					markDone(t, p)
				}
				before := planBytes(t, c)
				stopped := errors.New("simulated interruption")
				p.checkpoint = func(at string) error {
					if at == point {
						return stopped
					}
					return nil
				}
				options := StartOptions{Branch: "recover", ID: "task-002", Again: done}
				if _, err := p.Start(ctx, options); !errors.Is(err, stopped) {
					t.Fatalf("start: %v", err)
				}
				saved := planBytes(t, c)
				if point != "committed" && !bytes.Equal(before, saved) {
					t.Fatal("premature transition")
				}
				if _, err := p.Start(ctx, options); !errors.Is(err, storage.ErrOperation) {
					t.Fatalf("repeat: %v", err)
				}
				if _, err := addFixtureTask(t, p, ctx, "blocked", "", task.Position{}); !errors.Is(err, storage.ErrOperation) {
					t.Fatalf("add: %v", err)
				}
				if _, pending, err := p.StatusState(ctx); err != nil || !pending {
					t.Fatalf("status: %t %v", pending, err)
				}
				p.checkpoint = nil
				if point == "created" {
					if _, err := p.RecoverStart(ctx); err == nil {
						t.Fatal("recovery guessed checkout")
					}
					// A user may finish the switch explicitly; recovery itself never does.
					testrepo.Run(t, c, "checkout", "recover")
				}
				backup, _ := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
				if recovered, err := p.RecoverStart(ctx); err != nil || !recovered {
					t.Fatalf("recover: %t %v", recovered, err)
				}
				if point == "prepared" && !bytes.Equal(before, planBytes(t, c)) {
					t.Fatal("no-effect recovery changed plan")
				}
				if point == "committed" {
					if !bytes.Equal(saved, planBytes(t, c)) {
						t.Fatal("recovery rewrote installed plan")
					}
					after, _ := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
					if !bytes.Equal(backup, after) {
						t.Fatal("recovery rewrote backup")
					}
				}
				if recovered, err := p.RecoverStart(ctx); err != nil || recovered {
					t.Fatalf("repeat recover: %t %v", recovered, err)
				}
				if point != "prepared" {
					plan, _ := p.Status(ctx)
					got, _ := plan.FindID("task-002")
					if got.Status != task.Active || len(got.Attempts) != map[bool]int{false: 1, true: 2}[done] {
						t.Fatalf("bad recovery: %+v", got)
					}
				}
			})
		}
	}
}
