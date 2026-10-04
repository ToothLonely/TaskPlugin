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
	"strings"
	"testing"
	"time"
)

func doctorFiles(t *testing.T, s *Store) map[string][]byte {
	t.Helper()
	files, err := s.repairFiles()
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestDoctorStorageReadsEveryDamagedArtifactWithoutWriting(t *testing.T) {
	s, _ := initialized(t)
	put(t, filepath.Join(s.dir, "write.lock"), []byte("unknown owner"))
	put(t, filepath.Join(s.dir, "plan.backup.json"), []byte("broken backup"))
	put(t, filepath.Join(s.dir, "operation.json"), []byte("broken journal"))
	put(t, filepath.Join(s.dir, "pending-interrupted.json"), []byte("evidence"))
	before := doctorFiles(t, s)
	d, err := s.Inspect(context.Background())
	if err != nil || d.Plan == nil {
		t.Fatalf("diagnosis: %+v %v", d, err)
	}
	found := map[string]bool{}
	for _, f := range d.Findings {
		if f.Problem && f.Next != "" {
			found[f.Name] = true
		}
	}
	for _, name := range []string{"write.lock", "plan.backup.json", "operation.json", "pending-interrupted.json"} {
		if !found[name] {
			t.Errorf("missing finding %s", name)
		}
	}
	if !reflect.DeepEqual(before, doctorFiles(t, s)) {
		t.Fatal("doctor changed artifacts")
	}
}

func TestDoctorJournalSnapshotChangedDuringInspection(t *testing.T) {
	for _, mode := range []string{"closed", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := initialized(t)
			ctx := context.Background()
			base := snapshot(t, s)
			if err := s.WithOperation(ctx, func(op *Operation) error {
				_, err := op.Prepare(ctx, base, added(t, base, "Pending"), "snapshot")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, "operation.json")
			data := read(t, path)
			if mode == "closed" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				put(t, path, []byte("new journal"))
			}
			before := doctorFiles(t, s)
			f := s.inspectJournal(data)
			if !f.Problem || !strings.Contains(f.Next, "повторите git task doctor") || !strings.Contains(f.Detail, "изменилось") {
				t.Fatalf("concurrent journal diagnosis: %+v", f)
			}
			if !reflect.DeepEqual(before, doctorFiles(t, s)) {
				t.Fatal("diagnosis changed files after concurrent journal change")
			}
		})
	}
}

func corruptedStore(t *testing.T) (*Store, []byte) {
	t.Helper()
	s, _ := initialized(t)
	base := snapshot(t, s)
	if _, err := s.Save(context.Background(), base, added(t, base, "Newer local work")); err != nil {
		t.Fatal(err)
	}
	backup := read(t, filepath.Join(s.dir, "plan.backup.json"))
	put(t, filepath.Join(s.dir, "plan.json"), []byte(`{"format":"git-task","schema_version":2,"team":`))
	return s, backup
}

func TestDoctorRestorePreservesOriginalAndBackup(t *testing.T) {
	s, backup := corruptedStore(t)
	ctx := context.Background()
	original := read(t, filepath.Join(s.dir, "plan.json"))
	r, err := s.PrepareRepair(ctx, "restore-backup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Description, "очередь") {
		t.Fatal("missing data-loss preview")
	}
	saved, err := s.ApplyRepair(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read(t, saved), original) || !bytes.Equal(read(t, filepath.Join(s.dir, "plan.json")), backup) || !bytes.Equal(read(t, filepath.Join(s.dir, "plan.backup.json")), backup) {
		t.Fatal("restore lost source or changed backup")
	}
	if _, err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareRepair(ctx, "restore-backup"); err == nil {
		t.Fatal("valid plan was offered for replacement")
	}
}

func TestDoctorRestoreRejectsRacesAndUnsafeStates(t *testing.T) {
	for _, mode := range []string{"original", "backup", "operation", "pending", "unknown-schema", "foreign", "candidate-race", "backup-race"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := corruptedStore(t)
			ctx := context.Background()
			if mode == "unknown-schema" {
				put(t, filepath.Join(s.dir, "plan.json"), []byte(`{"format":"git-task","schema_version":99}`))
			}
			if mode == "foreign" {
				put(t, filepath.Join(s.dir, "plan.json"), []byte(`{"format":"other","schema_version":2}`))
			}
			if mode == "operation" {
				put(t, filepath.Join(s.dir, "operation.json"), []byte("interrupted"))
			}
			if mode == "pending" {
				put(t, filepath.Join(s.dir, "pending-write.json"), []byte("interrupted"))
			}
			before := doctorFiles(t, s)
			r, err := s.PrepareRepair(ctx, "restore-backup")
			if mode == "unknown-schema" || mode == "foreign" || mode == "operation" || mode == "pending" {
				if err == nil {
					t.Fatal("unsafe preview accepted")
				}
				if !reflect.DeepEqual(before, doctorFiles(t, s)) {
					t.Fatal("preview changed files")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "original" || mode == "backup" {
				name := "plan.json"
				if mode == "backup" {
					name = "plan.backup.json"
				}
				put(t, filepath.Join(s.dir, name), append(before[name], '\n'))
				expected := doctorFiles(t, s)
				if _, err := s.ApplyRepair(ctx, r); !errors.Is(err, ErrConflict) {
					t.Fatalf("race: %v", err)
				}
				if !reflect.DeepEqual(expected, doctorFiles(t, s)) {
					t.Fatal("race changed source")
				}
				return
			}
			s.checkpoint = func(point string) error {
				if point == "restore-candidate" {
					name := "plan.json"
					if mode == "backup-race" {
						name = "plan.backup.json"
					}
					put(t, filepath.Join(s.dir, name), append(before[name], '\n'))
				}
				return nil
			}
			path, err := s.ApplyRepair(ctx, r)
			if !errors.Is(err, ErrConflict) || path == "" {
				t.Fatalf("in-flight conflict: %q %v", path, err)
			}
			if !bytes.Equal(read(t, path), before["plan.json"]) {
				t.Fatal("original lost")
			}
		})
	}
}

func TestDoctorLockOwnerProcess(t *testing.T) {
	if os.Getenv("GIT_TASK_DOCTOR_CHILD") == "1" {
		return
	}
	s, _ := initialized(t)
	ctx := context.Background()
	unlock, err := s.lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareRepair(ctx, "unlock"); err == nil {
		t.Fatal("live owner accepted")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
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
	data := []byte(fmt.Sprintf("git-task write lock\npid=%d\ncreated=%s\nhost=%s\n", pid, time.Now().UTC().Format(time.RFC3339Nano), host))
	put(t, filepath.Join(s.dir, "write.lock"), data)
	r, err := s.PrepareRepair(ctx, "unlock")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(s.dir, "write.lock"), append(append([]byte{}, data...), '\n'))
	if _, err := s.ApplyRepair(ctx, r); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed lock accepted: %v", err)
	}
	put(t, filepath.Join(s.dir, "write.lock"), data)
	r, err = s.PrepareRepair(ctx, "unlock")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyRepair(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{[]byte("git-task write lock\npid=123\ncreated=old\n"), []byte(fmt.Sprintf("git-task write lock\npid=%d\ncreated=%s\nhost=other-computer\n", pid, time.Now().UTC().Format(time.RFC3339Nano)))} {
		put(t, filepath.Join(s.dir, "write.lock"), data)
		if _, err := s.PrepareRepair(ctx, "unlock"); err == nil {
			t.Fatal("unknown owner accepted")
		}
		if !bytes.Equal(read(t, filepath.Join(s.dir, "write.lock")), data) {
			t.Fatal("lock changed")
		}
	}
}

func TestDoctorRecoveryGateBlocksWriter(t *testing.T) {
	s, _ := initialized(t)
	base := snapshot(t, s)
	put(t, filepath.Join(s.dir, "recovery.lock"), []byte("recovery in progress"))
	if _, err := s.Save(context.Background(), base, added(t, base, "Blocked")); err == nil {
		t.Fatal("writer bypassed recovery gate")
	}
	if !bytes.Equal(base.raw, read(t, filepath.Join(s.dir, "plan.json"))) {
		t.Fatal("blocked writer changed plan")
	}
}

func TestDoctorRestoreInterruptedAfterInstallKeepsOriginal(t *testing.T) {
	s, backup := corruptedStore(t)
	ctx := context.Background()
	original := read(t, filepath.Join(s.dir, "plan.json"))
	r, err := s.PrepareRepair(ctx, "restore-backup")
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("after installed")
	s.checkpoint = func(point string) error {
		if point == "restore-installed" {
			return stop
		}
		return nil
	}
	path, err := s.ApplyRepair(ctx, r)
	if !errors.Is(err, stop) || !bytes.Equal(read(t, path), original) || !bytes.Equal(read(t, filepath.Join(s.dir, "plan.json")), backup) {
		t.Fatalf("installed restore lost data: %q %v", path, err)
	}
	s.checkpoint = nil
	if _, err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareRepair(ctx, "restore-backup"); err == nil {
		t.Fatal("repeated restore would overwrite valid data")
	}
}
