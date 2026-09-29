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
	"git-task/internal/transfer"
)

func importFixture(t *testing.T) (*Plans, *git.Client) {
	t.Helper()
	c := testrepo.New(t)
	repo, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := &Plans{git: c, store: storage.New(repo.Root, repo.ExcludePath, c)}
	if _, _, err = p.Init(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	return p, c
}

func importSource(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportPreviewApplyAndRepeat(t *testing.T) {
	t.Parallel()
	p, c := importFixture(t)
	ctx := context.Background()
	source := importSource(t, []byte("- [ ] Первая 🙂\n- [x] Готовая\n- [ ] Первая 🙂\n"))
	before := planBytes(t, c)
	preview, err := p.PrepareImport(ctx, source, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, planBytes(t, c)) || len(preview.Plan.Tasks) != 3 || preview.Destination != filepath.Join(c.Dir, ".git-task", "plan.json") {
		t.Fatalf("preview mutation: %+v", preview)
	}
	preview.Plan.Tasks[0].Title = "Изменение публичного предпросмотра"
	if err = p.ApplyImport(ctx, preview); err != nil {
		t.Fatal(err)
	}
	stored, err := p.Status(ctx)
	if err != nil || stored.Tasks[0].Title != "Первая 🙂" || stored.Tasks[1].Status != task.Done {
		t.Fatalf("stored=%+v %v", stored, err)
	}
	after := planBytes(t, c)
	if _, err = p.PrepareImport(ctx, source, "markdown"); err == nil {
		t.Fatal("repeated import accepted")
	}
	if err = p.ApplyImport(ctx, preview); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("repeated apply: %v", err)
	}
	if !bytes.Equal(after, planBytes(t, c)) {
		t.Fatal("repeat changed plan")
	}
	backup, err := os.ReadFile(filepath.Join(c.Dir, ".git-task", "plan.backup.json"))
	if err != nil || !bytes.Equal(backup, before) {
		t.Fatalf("backup: %v", err)
	}
	unchanged, err := os.ReadFile(source)
	if err != nil || string(unchanged) != "- [ ] Первая 🙂\n- [x] Готовая\n- [ ] Первая 🙂\n" {
		t.Fatalf("source changed: %v", err)
	}
}

func TestImportConflictsCancellationAndArchivedDestination(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cancel", "external bytes", "new task", "archived"} {
		t.Run(mode, func(t *testing.T) {
			p, c := importFixture(t)
			ctx := context.Background()
			preview, err := p.PrepareImport(ctx, importSource(t, []byte("- [ ] New")), "markdown")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "external bytes":
				if err = os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), append(planBytes(t, c), '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				editPlan(t, p, func(plan *task.Plan) error {
					added, err := plan.Add("Чужая", "", task.Position{})
					if err == nil && mode == "archived" {
						_, err = plan.Archive(added.ID)
					}
					return err
				})
			}
			before := planBytes(t, c)
			err = p.ApplyImport(ctx, preview)
			want := storage.ErrConflict
			if mode == "cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || !bytes.Equal(before, planBytes(t, c)) {
				t.Fatalf("%s: %v", mode, err)
			}
			if mode == "archived" {
				if _, err = p.PrepareImport(context.Background(), importSource(t, []byte("- [ ] X")), "markdown"); err == nil {
					t.Fatal("archived plan treated as empty")
				}
			}
		})
	}
}

func TestImportRejectsWholeDamagedInputAndStorage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"middle markdown", "broken json", "version", "directory", "corrupt destination", "pending", "locked"} {
		t.Run(mode, func(t *testing.T) {
			p, c := importFixture(t)
			data := []byte("- [ ] Good\n- [x] Good\n  - [ ] Nested\n- [ ] Last")
			format := "markdown"
			if mode == "broken json" || mode == "version" {
				format = "json"
				data = []byte(`{"format":"git-task","schema_version":2,"revision":0,"target_branch":"main","order":[],"tasks":[],"last_event":0}`)
				if mode == "broken json" {
					data = data[:len(data)/2]
				}
			}
			source := importSource(t, data)
			switch mode {
			case "directory":
				source = filepath.Dir(source)
			case "corrupt destination":
				if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "plan.json"), []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			case "pending", "locked":
				name := "operation.json"
				if mode == "locked" {
					name = "write.lock"
				}
				if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", name), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := planBytes(t, c)
			if preview, err := p.PrepareImport(context.Background(), source, format); err == nil || preview != nil {
				t.Fatalf("accepted %s: %+v %v", mode, preview, err)
			}
			if !bytes.Equal(before, planBytes(t, c)) {
				t.Fatal("damaged input changed destination")
			}
			if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "plan.backup.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("partial write backup: %v", err)
			}
		})
	}
}

func TestJSONImportExportPreservesStateWithoutChangingGit(t *testing.T) {
	t.Parallel()
	p, c := importFixture(t)
	testrepo.Commit(t, c)
	plan, err := transfer.DecodeMarkdown([]byte("- [x] Готовая\n- [ ] Пауза\n- [ ] Архив"), "other-target")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"task-001", "task-002"} {
		branch := "missing-" + id
		a := task.Attempt{ID: "new-" + id, Branch: branch, OriginalBranch: branch, TargetBranch: plan.TargetBranch, BaseCommit: strings.Repeat("a", 40), StartedAt: &now}
		if _, err = plan.Start(id, a, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = plan.Pause("task-002"); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Archive("task-003"); err != nil {
		t.Fatal(err)
	}
	data, err := transfer.EncodeJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	refs, err := c.Run(ctx, "show-ref")
	if err != nil {
		t.Fatal(err)
	}
	status, err := c.Run(ctx, "status", "--porcelain=v1")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.PrepareImport(ctx, importSource(t, data), "json")
	if err != nil || preview.Plan.TargetBranch != "other-target" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if err = p.ApplyImport(ctx, preview); err != nil {
		t.Fatal(err)
	}
	got, err := p.Status(ctx)
	if err != nil || !reflect.DeepEqual(got, plan) {
		t.Fatalf("lost data: %+v %v", got, err)
	}
	exported, err := p.Export(ctx, "json")
	if err != nil || !bytes.Equal(exported, data) {
		t.Fatalf("round-trip: %s %v", exported, err)
	}
	refsAfter, err := c.Run(ctx, "show-ref")
	if err != nil || !bytes.Equal(refsAfter.Stdout, refs.Stdout) {
		t.Fatalf("refs changed: %v", err)
	}
	statusAfter, err := c.Run(ctx, "status", "--porcelain=v1")
	if err != nil || !bytes.Equal(statusAfter.Stdout, status.Stdout) {
		t.Fatalf("index/worktree changed: %v", err)
	}
}

func TestJSONImportPreservesZeroRevisionAndSettings(t *testing.T) {
	t.Parallel()
	p, _ := importFixture(t)
	plan, err := task.NewPlan("other-target")
	if err != nil {
		t.Fatal(err)
	}
	data, err := transfer.EncodeJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.PrepareImport(context.Background(), importSource(t, data), "json")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.ApplyImport(context.Background(), preview); err != nil {
		t.Fatal(err)
	}
	got, err := p.Status(context.Background())
	if err != nil || !reflect.DeepEqual(got, plan) {
		t.Fatalf("zero revision/settings lost: %+v %v", got, err)
	}
}

func TestExportNoOverwriteProtectedPathsAndCancellation(t *testing.T) {
	t.Parallel()
	p, c := importFixture(t)
	ctx := context.Background()
	data, err := p.Export(ctx, "json")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(c.Dir, "snapshot.json")
	if err = p.ExportFile(ctx, output, data); err != nil {
		t.Fatal(err)
	}
	if err = p.ExportFile(ctx, output, []byte("overwrite")); err == nil {
		t.Fatal("existing export overwritten")
	}
	saved, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(saved, data) {
		t.Fatalf("existing export lost: %v", err)
	}
	for _, path := range []string{filepath.Join(c.Dir, ".git-task", "new.json"), filepath.Join(c.Dir, ".git", "new.json"), filepath.Join(c.Dir, ".git-task", "plan.json"), filepath.Join(c.Dir, "missing", "file"), c.Dir} {
		if err = p.ExportFile(ctx, path, data); err == nil {
			t.Fatalf("unsafe export %s", path)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = p.ExportFile(canceled, filepath.Join(c.Dir, "cancel.json"), data); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".git-task-export-") || entry.Name() == "cancel.json" {
			t.Fatalf("export residue: %s", entry.Name())
		}
	}
}

func TestExportRejectsPendingAndCorruptStorage(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"operation.json", "write.lock", "pending-test.json", "plan.json"} {
		t.Run(name, func(t *testing.T) {
			p, c := importFixture(t)
			if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", name), []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"json", "markdown"} {
				if data, err := p.Export(context.Background(), format); err == nil || data != nil {
					t.Fatalf("exported %s during %s: %s %v", format, name, data, err)
				}
			}
		})
	}
}
