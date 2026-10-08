package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"git-task/internal/task"
)

func appliedTemplateStore(t *testing.T) (*Store, []byte) {
	t.Helper()
	s, _ := fixture(t)
	ctx := context.Background()
	if _, err := s.InitTemplate(ctx, false); err != nil {
		t.Fatal(err)
	}
	filled := []byte(`{"target_branch":"main","tasks":[{"title":"First"}]}`)
	put(t, filepath.Join(s.dir, "plan.json"), filled)
	if _, err := s.InitTemplate(ctx, true); err != nil {
		t.Fatal(err)
	}
	return s, filled
}

func absentTemplateLock(t *testing.T) []byte {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestDoctorLockOwnerProcess$")
	child.Env = append(os.Environ(), "GIT_TASK_DOCTOR_CHILD=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return []byte(fmt.Sprintf("git-task write lock\npid=%d\ncreated=%s\nhost=%s\n", pid, time.Now().UTC().Format(time.RFC3339Nano), host))
}

func TestDoctorRepairAfterTemplateApply(t *testing.T) {
	for _, action := range []string{"restore-backup", "unlock"} {
		t.Run(action, func(t *testing.T) {
			s, filled := appliedTemplateStore(t)
			ctx := context.Background()
			path := filepath.Join(s.dir, "plan.json")
			var original, backup []byte
			if action == "restore-backup" {
				base := snapshot(t, s)
				if _, err := s.Save(ctx, base, added(t, base, "Second")); err != nil {
					t.Fatal(err)
				}
				backup = read(t, filepath.Join(s.dir, "plan.backup.json"))
				original = []byte(`{"format":"git-task","schema_version":3,"tasks":`)
				put(t, path, original)
			} else {
				original = read(t, path)
				put(t, filepath.Join(s.dir, "write.lock"), absentTemplateLock(t))
			}
			before := doctorFiles(t, s)
			r, err := s.PrepareRepair(ctx, action)
			if err != nil || r.NoChange || !reflect.DeepEqual(before, doctorFiles(t, s)) {
				t.Fatalf("preview: %+v %v", r, err)
			}
			saved, err := s.ApplyRepair(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(filled, read(t, filepath.Join(s.dir, "plan.template.json"))) {
				t.Fatal("repair changed the original template")
			}
			if action == "restore-backup" {
				if !bytes.Equal(original, read(t, saved)) || !bytes.Equal(backup, read(t, path)) || !bytes.Equal(backup, read(t, filepath.Join(s.dir, "plan.backup.json"))) {
					t.Fatal("restore lost the source or backup")
				}
			} else if !bytes.Equal(original, read(t, path)) {
				t.Fatal("unlock changed the plan")
			}
			if _, err := s.Load(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDoctorRepairDetectsTemplateChanges(t *testing.T) {
	for _, action := range []string{"restore-backup", "unlock"} {
		t.Run(action, func(t *testing.T) {
			s, filled := appliedTemplateStore(t)
			ctx := context.Background()
			if action == "restore-backup" {
				base := snapshot(t, s)
				if _, err := s.Save(ctx, base, added(t, base, "Second")); err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(s.dir, "plan.json"), []byte("corrupt"))
			} else {
				put(t, filepath.Join(s.dir, "write.lock"), absentTemplateLock(t))
			}
			r, err := s.PrepareRepair(ctx, action)
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(s.dir, "plan.template.json"), append(filled, '\n'))
			before := doctorFiles(t, s)
			if _, err := s.ApplyRepair(ctx, r); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed template ignored: %v", err)
			}
			if !reflect.DeepEqual(before, doctorFiles(t, s)) {
				t.Fatal("conflict changed storage")
			}
		})
	}
}

func TestDoctorRestoreDoesNotUseTemplateAsPlanBackup(t *testing.T) {
	s, _ := appliedTemplateStore(t)
	ctx := context.Background()
	put(t, filepath.Join(s.dir, "plan.json"), []byte("corrupt"))
	before := doctorFiles(t, s)
	if _, err := s.PrepareRepair(ctx, "restore-backup"); err == nil {
		t.Fatal("template accepted as backup of the working plan")
	}
	if !reflect.DeepEqual(before, doctorFiles(t, s)) {
		t.Fatal("failed preview changed files")
	}
	if _, err := task.ReadTemplate(before["plan.template.json"]); err != nil {
		t.Fatal(err)
	}
}
