package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/hooks"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestDoctorReviewUnsupportedRepositoriesPreserveFiles(t *testing.T) {
	for _, mode := range []string{"bare", "shallow", "primary-worktree", "linked-worktree"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			repo, err := c.Discover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "bare" {
				root := filepath.Join(t.TempDir(), "bare.git")
				testrepo.Run(t, c, "init", "--bare", "--template=", root)
				before := doctorReviewFiles(t, root)
				if _, err := Open(ctx, root); !errors.Is(err, git.ErrNotWorkTree) {
					t.Fatalf("bare accepted: %v", err)
				}
				if !reflect.DeepEqual(before, doctorReviewFiles(t, root)) {
					t.Fatal("bare discovery changed files")
				}
				return
			}
			if mode == "shallow" {
				commit := testrepo.Run(t, c, "rev-parse", "HEAD")
				if err := os.WriteFile(filepath.Join(repo.GitDir, "shallow"), commit, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				linked := filepath.Join(t.TempDir(), "linked")
				testrepo.Run(t, c, "worktree", "add", "-b", "review-linked", linked)
				if mode == "linked-worktree" {
					client := *c
					client.Dir = linked
					linkedRepo, err := client.Discover(ctx)
					if err != nil {
						t.Fatal(err)
					}
					p = &Plans{git: &client, store: storage.New(linkedRepo.Root, linkedRepo.ExcludePath, &client)}
				}
			}
			before := doctorReviewFiles(t, c.Dir)
			current := doctorReviewFiles(t, p.git.Dir)
			d, err := doctorDiagnosis(ctx, p)
			if err != nil || !d.HasProblems() {
				t.Fatalf("unsupported diagnosis: %+v %v", d, err)
			}
			for _, action := range []string{"recover-start", "restore-backup", "unlock"} {
				if _, err := p.PrepareRepair(ctx, action); err == nil {
					t.Fatalf("unsupported repair accepted: %s", action)
				}
			}
			if !reflect.DeepEqual(before, doctorReviewFiles(t, c.Dir)) || !reflect.DeepEqual(current, doctorReviewFiles(t, p.git.Dir)) {
				t.Fatal("unsupported diagnosis or repair changed files")
			}
		})
	}
}

func doctorReviewFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		result[path] = data
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDoctorReviewExactFilesAndChangedHooks(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	repo, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, c, "config", "--local", "core.hooksPath", filepath.Join(repo.GitDir, "hooks"))
	before := doctorReviewFiles(t, c.Dir)
	if _, err := doctorDiagnosis(ctx, p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, doctorReviewFiles(t, c.Dir)) {
		t.Fatal("doctor changed repository bytes, including the index")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (hooks.Installer{Git: c, Binary: binary}).Install(ctx); err != nil {
		t.Fatal(err)
	}
	dir, err := c.HooksDirectory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "post-commit")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0700); err != nil {
		t.Fatal(err)
	}
	before = doctorReviewFiles(t, c.Dir)
	d, err := doctorDiagnosis(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range d.Findings {
		if finding.Name == "hooks" && finding.Problem && strings.Contains(finding.Detail, "изменён") {
			found = true
		}
	}
	if !found || !reflect.DeepEqual(before, doctorReviewFiles(t, c.Dir)) {
		t.Fatal("doctor missed or modified changed hooks")
	}
}

func TestDoctorReviewActualStartCrashRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "checked-out", "committed"} {
		t.Run(phase, func(t *testing.T) {
			p, c := startFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			original := planBytes(t, c)
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartCrashChild$")
			child.Env = append(append([]string{}, c.Env...), "GIT_TASK_CRASH_ROOT="+c.Dir, "GIT_TASK_CRASH_PHASE="+phase)
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("crash process: %v %s", err, output)
			}
			before := doctorArtifacts(t, c)
			if _, err := doctorDiagnosis(ctx, p); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, doctorArtifacts(t, c)) {
				t.Fatal("doctor changed crashed operation")
			}
			preview, err := p.PrepareRepair(ctx, "recover-start")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.ApplyRepair(ctx, preview); err != nil {
				t.Fatal(err)
			}
			s, err := p.store.Load(ctx)
			if err != nil || s.PendingOperation {
				t.Fatalf("recovered snapshot: %v", err)
			}
			item, err := s.Plan.FindID("task-002")
			if err != nil {
				t.Fatal(err)
			}
			if phase == "prepared" {
				if !bytes.Equal(original, planBytes(t, c)) || item.Status != task.Todo || len(item.Attempts) != 0 {
					t.Fatal("no-effect recovery changed plan")
				}
			} else if item.Status != task.Active || len(item.Attempts) != 1 {
				t.Fatalf("lost or duplicated attempt: %+v", item)
			}
			if phase == "committed" && !bytes.Equal(before["plan.json"], planBytes(t, c)) {
				t.Fatal("installed plan was rewritten")
			}
			if _, err := os.Stat(filepath.Join(c.Dir, ".git-task", "write.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("orphan lock remains: %v", err)
			}
			after := doctorArtifacts(t, c)
			for _, key := range []string{"show-ref", "symbolic-ref HEAD", "ls-files --stage -z", "status --porcelain=v1 -z"} {
				if !bytes.Equal(before[key], after[key]) {
					t.Fatalf("repair changed Git state: %s", key)
				}
			}
		})
	}
}
