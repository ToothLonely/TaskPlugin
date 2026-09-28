package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func fixture(t *testing.T) (*Store, *git.Client) {
	t.Helper()
	c := testrepo.New(t)
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return New(repo.Root, repo.ExcludePath, c), c
}

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func initialized(t *testing.T) (*Store, *git.Client) {
	t.Helper()
	s, c := fixture(t)
	if created, err := s.Init(context.Background(), "main"); err != nil || !created {
		t.Fatalf("Init: %v %v", created, err)
	}
	return s, c
}

func snapshot(t *testing.T, s *Store) Snapshot {
	t.Helper()
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func added(t *testing.T, snap Snapshot, title string) task.Plan {
	t.Helper()
	p := snap.Plan
	if _, err := p.Add(title, "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInitNoIdentityExcludeAndNoOp(t *testing.T) {
	s, c := fixture(t)
	if err := os.MkdirAll(filepath.Dir(s.excludePath), 0700); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("# чужие правила\r\n*.secret")
	put(t, s.excludePath, foreign)
	if created, err := s.Init(context.Background(), "main"); err != nil || !created {
		t.Fatalf("%v %v", created, err)
	}
	exclusion := read(t, s.excludePath)
	if !bytes.Equal(exclusion, append(foreign, []byte("\n/.git-task/\n")...)) {
		t.Fatalf("exclude=%q", exclusion)
	}
	path := filepath.Join(s.dir, "plan.json")
	old := read(t, path)
	stamp := time.Unix(1000000000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if created, err := s.Init(context.Background(), "main"); err != nil || created {
		t.Fatalf("repeat: %v %v", created, err)
	}
	snap := snapshot(t, s)
	if changed, err := s.Save(context.Background(), snap, snap.Plan); err != nil || changed {
		t.Fatalf("noop: %v %v", changed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) || !bytes.Equal(old, read(t, path)) || !bytes.Equal(exclusion, read(t, s.excludePath)) {
		t.Fatal("no-op changed files")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "plan.backup.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected backup: %v", err)
	}
	if _, err := s.Init(context.Background(), "other"); err == nil {
		t.Fatal("target silently changed")
	}
	if output := testrepo.Run(t, c, "status", "--porcelain"); len(output) != 0 {
		t.Fatalf("status=%s", output)
	}
	testrepo.Run(t, c, "add", ".")
	if output := testrepo.Run(t, c, "ls-files"); len(output) != 0 {
		t.Fatalf("tracked=%s", output)
	}
	if _, err := c.Run(context.Background(), "config", "user.name"); err == nil {
		t.Fatal("identity unexpectedly configured")
	}
	put(t, s.excludePath, foreign)
	if _, err := s.Init(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, read(t, path)) {
		t.Fatal("exclude repair rewrote plan")
	}
}

func TestSaveBackupAndExternalConflict(t *testing.T) {
	s, _ := initialized(t)
	snap := snapshot(t, s)
	old := read(t, filepath.Join(s.dir, "plan.json"))
	next := added(t, snap, "Первая")
	if changed, err := s.Save(context.Background(), snap, next); err != nil || !changed {
		t.Fatalf("save: %v %v", changed, err)
	}
	if !bytes.Equal(old, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
		t.Fatal("backup differs from source bytes")
	}
	current := snapshot(t, s)
	if len(current.Plan.Tasks) != 1 || current.Plan.Tasks[0].Title != "Первая" {
		t.Fatal("lost task")
	}
	path := filepath.Join(s.dir, "plan.json")
	external := append(read(t, path), ' ')
	put(t, path, external)
	if _, err := s.Save(context.Background(), current, added(t, current, "Вторая")); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if !bytes.Equal(external, read(t, path)) {
		t.Fatal("external edit overwritten")
	}
	files, err := filepath.Glob(filepath.Join(s.dir, "pending-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("candidates=%v %v", files, err)
	}
	if p, err := decode(read(t, files[0])); err != nil || len(p.Tasks) != 2 {
		t.Fatalf("candidate: %v %v", p, err)
	}
}

func TestDirtyTreeIndexHistoryAndBranches(t *testing.T) {
	s, c := fixture(t)
	code := filepath.Join(c.Dir, "code.txt")
	put(t, code, []byte("base\n"))
	testrepo.Run(t, c, "add", ".")
	testrepo.Commit(t, c)
	testrepo.Run(t, c, "branch", "other")
	put(t, code, []byte("staged\n"))
	testrepo.Run(t, c, "add", "code.txt")
	put(t, code, []byte("unstaged\n"))
	indexPath := filepath.Join(c.Dir, ".git", "index")
	index := read(t, indexPath)
	head := testrepo.Run(t, c, "rev-parse", "HEAD")
	refs := testrepo.Run(t, c, "show-ref")
	if _, err := s.Init(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, s)
	if _, err := s.Save(context.Background(), snap, added(t, snap, "Задача")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(index, read(t, indexPath)) || string(read(t, code)) != "unstaged\n" || !bytes.Equal(head, testrepo.Run(t, c, "rev-parse", "HEAD")) || !bytes.Equal(refs, testrepo.Run(t, c, "show-ref")) {
		t.Fatal("storage changed code/index/refs")
	}
	plan := read(t, filepath.Join(s.dir, "plan.json"))
	testrepo.Run(t, c, "switch", "other")
	if !bytes.Equal(plan, read(t, filepath.Join(s.dir, "plan.json"))) {
		t.Fatal("checkout changed plan")
	}
	if p := snapshot(t, s).Plan; len(p.Tasks) != 1 {
		t.Fatal("checkout lost task")
	}
}

func TestUnsafePathsAndDataPreserved(t *testing.T) {
	for _, kind := range []string{"foreign-file", "foreign-dir", "corrupt", "future", "tracked-index", "tracked-head", "exclude-directory", "ignore-override"} {
		t.Run(kind, func(t *testing.T) {
			s, c := fixture(t)
			if err := os.MkdirAll(filepath.Dir(s.excludePath), 0700); err != nil {
				t.Fatal(err)
			}
			exclude := []byte("# preserve\n")
			put(t, s.excludePath, exclude)
			plan, _ := task.NewPlan("main")
			valid, err := encode(plan)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "foreign-file":
				put(t, s.dir, []byte("foreign"))
			case "foreign-dir", "corrupt", "future", "tracked-index", "tracked-head":
				if err := os.Mkdir(s.dir, 0700); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "foreign-dir":
					put(t, filepath.Join(s.dir, "foreign.txt"), []byte("keep"))
				case "corrupt":
					put(t, filepath.Join(s.dir, "plan.json"), []byte("{"))
				case "future":
					put(t, filepath.Join(s.dir, "plan.json"), bytes.Replace(valid, []byte(`"schema_version": 1`), []byte(`"schema_version": 99`), 1))
				default:
					put(t, filepath.Join(s.dir, "plan.json"), valid)
					testrepo.Run(t, c, "add", ".git-task")
					if kind == "tracked-head" {
						testrepo.Commit(t, c)
						testrepo.Run(t, c, "rm", "--cached", ".git-task/plan.json")
					}
				}
			case "exclude-directory":
				if err := os.Remove(s.excludePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(s.excludePath, 0700); err != nil {
					t.Fatal(err)
				}
			case "ignore-override":
				put(t, filepath.Join(c.Dir, ".gitignore"), []byte("!/.git-task/\n"))
			}
			before, _ := os.ReadFile(filepath.Join(s.dir, "plan.json"))
			if _, err := s.Init(context.Background(), "main"); err == nil {
				t.Fatal("unsafe init succeeded")
			}
			after, _ := os.ReadFile(filepath.Join(s.dir, "plan.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("data overwritten")
			}
			if kind != "exclude-directory" && kind != "ignore-override" && !bytes.Equal(exclude, read(t, s.excludePath)) {
				t.Fatal("exclude touched before validation")
			}
		})
	}
}

func TestSaveValidationAndFailureBoundaries(t *testing.T) {
	for _, point := range []string{"candidate", "backup", "installed", "external-at-backup", "invalid", "revision", "corrupt-backup", "canceled"} {
		t.Run(point, func(t *testing.T) {
			s, _ := initialized(t)
			snap := snapshot(t, s)
			next := added(t, snap, "new")
			path := filepath.Join(s.dir, "plan.json")
			old := read(t, path)
			injected := errors.New("injected failure")
			ctx := context.Background()
			switch point {
			case "invalid":
				next.Tasks[0].Title = ""
			case "revision":
				next.Revision = snap.Plan.Revision
			case "corrupt-backup":
				put(t, filepath.Join(s.dir, "plan.backup.json"), []byte("corrupt"))
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			default:
				s.checkpoint = func(name string) error {
					if point == "external-at-backup" && name == "backup" {
						put(t, path, []byte("external invalid bytes"))
						return nil
					}
					if name == point {
						return injected
					}
					return nil
				}
			}
			if _, err := s.Save(ctx, snap, next); err == nil {
				t.Fatal("failure silently succeeded")
			}
			actual := read(t, path)
			switch point {
			case "installed":
				if p, err := decode(actual); err != nil || len(p.Tasks) != 1 {
					t.Fatalf("installed: %v %v", p, err)
				}
			case "external-at-backup":
				if string(actual) != "external invalid bytes" {
					t.Fatal("external write overwritten")
				}
			default:
				if !bytes.Equal(old, actual) {
					t.Fatal("original lost")
				}
			}
			if point == "backup" || point == "installed" || point == "external-at-backup" {
				if !bytes.Equal(old, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
					t.Fatal("backup lost")
				}
			}
		})
	}
}
