package app

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

	"git-task/internal/git"
	"git-task/internal/hooks"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func doctorDiagnosis(ctx context.Context, p *Plans) (Diagnosis, error) {
	return p.Doctor(ctx, func(ctx context.Context, _ string) ([]string, error) {
		return (hooks.Installer{Git: p.git}).Diagnose(ctx)
	})
}

func TestDoctorHookDiagnosisBoundary(t *testing.T) {
	for _, mode := range []string{"result", "error", "missing"} {
		t.Run(mode, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			before := doctorArtifacts(t, c)
			calls := 0
			var diagnose func(context.Context, string) ([]string, error)
			if mode != "missing" {
				diagnose = func(received context.Context, root string) ([]string, error) {
					calls++
					if received != ctx || root != c.Dir {
						t.Fatalf("wrong hook boundary: %s", root)
					}
					if mode == "error" {
						return nil, errors.New("hook diagnostic failure")
					}
					return []string{"provided-hook"}, nil
				}
			}
			d, err := p.Doctor(ctx, diagnose)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, f := range d.Findings {
				if f.Name != "hooks" {
					continue
				}
				found = true
				if f.Problem != (mode != "result") || f.Next == "" {
					t.Fatalf("hook finding: %+v", f)
				}
				if mode == "result" && !strings.Contains(f.Detail, "provided-hook") {
					t.Fatalf("supplied result missing: %+v", f)
				}
			}
			if !found || mode != "missing" && calls != 1 || mode == "missing" && calls != 0 {
				t.Fatalf("diagnosis calls=%d found=%t", calls, found)
			}
			if !reflect.DeepEqual(before, doctorArtifacts(t, c)) {
				t.Fatal("hook diagnostic mutated repository")
			}
		})
	}
}

func doctorArtifacts(t *testing.T, c *git.Client) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	root := filepath.Join(c.Dir, ".git-task")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[e.Name()] = data
	}
	for _, args := range [][]string{{"show-ref"}, {"symbolic-ref", "HEAD"}, {"ls-files", "--stage", "-z"}, {"status", "--porcelain=v1", "-z"}} {
		result[strings.Join(args, " ")] = testrepo.Run(t, c, args...)
	}
	return result
}

func assertDoctorQueueReadOnly(t *testing.T, p *Plans, c *git.Client) {
	t.Helper()
	before := doctorArtifacts(t, c)
	d, err := doctorDiagnosis(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range d.Findings {
		if f.Name == "неопубликованное действие" {
			found = true
		}
	}
	if !found || !reflect.DeepEqual(before, doctorArtifacts(t, c)) {
		t.Fatal("doctor lost or changed pending queue")
	}
}

func TestDoctorInterruptedStartRepairAndNoOp(t *testing.T) {
	for _, boundary := range []string{"checked-out", "committed"} {
		t.Run(boundary, func(t *testing.T) {
			p, c := startFixture(t)
			ctx := context.Background()
			stop := errors.New("interrupted")
			p.checkpoint = func(point string) error {
				if point == boundary {
					return stop
				}
				return nil
			}
			if _, err := p.Start(ctx, StartOptions{Branch: "recover-doctor", ID: "task-001"}); !errors.Is(err, stop) {
				t.Fatal(err)
			}
			p.checkpoint = nil
			before := doctorArtifacts(t, c)
			d, err := doctorDiagnosis(ctx, p)
			if err != nil || !d.HasProblems() {
				t.Fatalf("diagnosis: %+v %v", d, err)
			}
			if !reflect.DeepEqual(before, doctorArtifacts(t, c)) {
				t.Fatal("doctor mutated interrupted operation")
			}
			preview, err := p.PrepareRepair(ctx, "recover-start")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.ApplyRepair(ctx, preview); err != nil {
				t.Fatal(err)
			}
			s, err := p.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			item, err := s.Plan.FindID("task-001")
			if err != nil || item.Status != task.Active || len(item.Attempts) != 1 {
				t.Fatalf("repair duplicated/lost attempt: %+v %v", item, err)
			}
			if boundary == "committed" && (!bytes.Equal(before["plan.json"], planBytes(t, c)) || !bytes.Equal(before["plan.backup.json"], doctorArtifacts(t, c)["plan.backup.json"])) {
				t.Fatal("already installed plan was rewritten")
			}
			after := doctorArtifacts(t, c)
			preview, err = p.PrepareRepair(ctx, "recover-start")
			if err != nil || !preview.Storage.NoChange {
				t.Fatalf("repeat: %v", err)
			}
			if _, err := p.ApplyRepair(ctx, preview); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, doctorArtifacts(t, c)) {
				t.Fatal("no-op changed state")
			}
		})
	}
}

func TestDoctorRepairRejectsChangedPreview(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	p.checkpoint = func(point string) error {
		if point == "checked-out" {
			return errors.New("stop")
		}
		return nil
	}
	if _, err := p.Start(ctx, StartOptions{Branch: "preview-race", ID: "task-001"}); err == nil {
		t.Fatal("missing interruption")
	}
	p.checkpoint = nil
	preview, err := p.PrepareRepair(ctx, "recover-start")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, ".git-task", "operation.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	before := doctorArtifacts(t, c)
	if _, err := p.ApplyRepair(ctx, preview); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("changed journal accepted: %v", err)
	}
	if !reflect.DeepEqual(before, doctorArtifacts(t, c)) {
		t.Fatal("race lost evidence")
	}
}

func TestDoctorOrphanLockRecovery(t *testing.T) {
	if os.Getenv("GIT_TASK_DOCTOR_ORPHAN_CHILD") == "1" {
		return
	}
	p, c := startFixture(t)
	ctx := context.Background()
	p.checkpoint = func(point string) error {
		if point == "checked-out" {
			return errors.New("crash boundary")
		}
		return nil
	}
	if _, err := p.Start(ctx, StartOptions{Branch: "orphan-doctor", ID: "task-001"}); err == nil {
		t.Fatal("missing interruption")
	}
	p.checkpoint = nil
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestDoctorOrphanLockRecovery$")
	child.Env = append(os.Environ(), "GIT_TASK_DOCTOR_ORPHAN_CHILD=1")
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
	lock := []byte(fmt.Sprintf("git-task write lock\npid=%d\ncreated=%s\nhost=%s\n", pid, time.Now().UTC().Format(time.RFC3339Nano), host))
	if err := os.WriteFile(filepath.Join(c.Dir, ".git-task", "write.lock"), lock, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PrepareRepair(ctx, "unlock"); !errors.Is(err, storage.ErrOperation) {
		t.Fatalf("generic unlock bypassed journal: %v", err)
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
		t.Fatalf("orphan recovery: %v", err)
	}
	item, err := s.Plan.FindID("task-001")
	if err != nil || len(item.Attempts) != 1 || item.Status != task.Active {
		t.Fatalf("orphan recovery lost identity: %+v %v", item, err)
	}
}

func TestDoctorPendingLostAckAndForeignApproachStayReadOnly(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	item, err := f.a.Start(ctx, StartOptions{Branch: "alice-doctor", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	lost := errors.New("lost ack")
	f.a.checkpoint = func(point string) error {
		if point == "publish-after-push" {
			return lost
		}
		return nil
	}
	if err := f.a.Publish(ctx); !errors.Is(err, lost) {
		t.Fatalf("lost ack: %v", err)
	}
	f.a.checkpoint = nil
	before := doctorArtifacts(t, f.ca)
	d, err := doctorDiagnosis(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	pending := false
	for _, finding := range d.Findings {
		if finding.Name == "неопубликованное действие" {
			pending = true
		}
	}
	if !pending || d.HasProblems() {
		t.Fatalf("pending action treated as interrupted Git: %+v", d)
	}
	if !reflect.DeepEqual(before, doctorArtifacts(t, f.ca)) {
		t.Fatal("doctor changed lost-ack queue")
	}
	if err := f.b.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	otherBefore := doctorArtifacts(t, f.cb)
	d, err = doctorDiagnosis(ctx, f.b)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range d.Findings {
		if finding.Name == "подход" && strings.Contains(finding.Detail, item.ActiveAttempt.ID) {
			found = true
			if finding.Problem || !strings.Contains(finding.Detail, "local=false") || !strings.Contains(finding.Detail, "Alice") {
				t.Fatalf("foreign approach: %+v", finding)
			}
		}
	}
	if !found || !reflect.DeepEqual(otherBefore, doctorArtifacts(t, f.cb)) {
		t.Fatal("foreign approach lost or mutated")
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := f.a.store.Load(ctx)
	if err != nil || len(s.Plan.Team.Pending) != 0 {
		t.Fatalf("republish: %v", err)
	}
	final, err := s.Plan.FindID(f.id)
	if err != nil || len(final.Attempts) != 1 || final.Attempts[0].ID != item.ActiveAttempt.ID {
		t.Fatal("republish duplicated attempt")
	}
}

func TestDoctorUnavailableTransportAndCorruptSharedSnapshot(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Add(ctx, "offline doctor", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	tip, err := f.ca.CachedPlanTip(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}
	bad, err := f.ca.PlanCommit(ctx, tip, []byte(`{"format":"git-task","schema_version":99}`))
	if err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, f.ca, "update-ref", "refs/remotes/origin/git-task-plan", bad, tip)
	testrepo.Run(t, f.ca, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "absent-remote"))
	trace := filepath.Join(t.TempDir(), "git-trace.log")
	f.ca.Env = append(f.ca.Env, "GIT_TRACE="+trace)
	before := doctorArtifacts(t, f.ca)
	d, err := doctorDiagnosis(ctx, f.a)
	if err != nil || !d.HasProblems() {
		t.Fatalf("corrupt cached snapshot: %+v %v", d, err)
	}
	found := false
	for _, finding := range d.Findings {
		if finding.Name == "полученная служебная версия" && finding.Problem {
			found = true
		}
	}
	if !found || !reflect.DeepEqual(before, doctorArtifacts(t, f.ca)) {
		t.Fatal("doctor repaired corrupt snapshot or performed transport")
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git fetch ", "git push ", "git ls-remote ", "git checkout ", "git update-ref "} {
		if bytes.Contains(data, []byte(command)) {
			t.Fatalf("doctor performed %s: %s", command, data)
		}
	}
}

func TestDoctorRestoreKeepsBackupQueueAndPreservesNewerOriginal(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Add(ctx, "first offline", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	first, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := first.Plan.Team.Pending[0].ID
	if _, err := f.a.Add(ctx, "second offline", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.PrepareRepair(ctx, "restore-backup"); err == nil {
		t.Fatal("valid unpublished work offered for replacement")
	}
	original := planBytes(t, f.ca)
	broken := append(append([]byte{}, original...), []byte("broken trailing data")...)
	path := filepath.Join(f.ca.Dir, ".git-task", "plan.json")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := f.a.PrepareRepair(ctx, "restore-backup")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.a.ApplyRepair(ctx, preview)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(saved)
	if err != nil || !bytes.Equal(data, broken) {
		t.Fatal("newer queued data was lost")
	}
	restored, err := f.a.store.Load(ctx)
	if err != nil || len(restored.Plan.Team.Pending) != 1 || restored.Plan.Team.Pending[0].ID != id {
		t.Fatalf("backup queue lost: %v", err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := f.a.store.Load(ctx)
	if err != nil || len(final.Plan.Team.Pending) != 0 || len(final.Plan.Tasks) != 2 {
		t.Fatalf("queue restoration publication: %v", err)
	}
}

func TestDoctorDamagedQueueAndMigrationSourcesArePreserved(t *testing.T) {
	for _, mode := range []string{"queue", "migration"} {
		t.Run(mode, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			if _, err := f.a.Add(ctx, "pending", "", task.Position{}); err != nil {
				t.Fatal(err)
			}
			if mode == "queue" {
				data := planBytes(t, f.ca)
				s, err := f.a.store.Load(ctx)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`"id": "`+s.Plan.Team.Pending[0].ID+`"`), []byte(`"id": ""`), 1)
				if err := os.WriteFile(filepath.Join(f.ca.Dir, ".git-task", "plan.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(f.ca.Dir, ".git-task", "plan.schema-1.json"), []byte("interrupted migration"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := doctorArtifacts(t, f.ca)
			d, err := doctorDiagnosis(ctx, f.a)
			if err != nil || !d.HasProblems() {
				t.Fatalf("damage diagnosis: %+v %v", d, err)
			}
			if !reflect.DeepEqual(before, doctorArtifacts(t, f.ca)) {
				t.Fatal("doctor erased damaged queue or migration")
			}
		})
	}
}

func TestDoctorUnsupportedEnvironmentRefusesRepair(t *testing.T) {
	p, c := startFixture(t)
	ctx := context.Background()
	testrepo.Run(t, c, "config", "extensions.partialClone", "origin")
	before := doctorArtifacts(t, c)
	d, err := doctorDiagnosis(ctx, p)
	if err != nil || !d.HasProblems() {
		t.Fatalf("unsupported: %+v %v", d, err)
	}
	if _, err := p.PrepareRepair(ctx, "restore-backup"); err == nil {
		t.Fatal("repair accepted partial clone")
	}
	if !reflect.DeepEqual(before, doctorArtifacts(t, c)) {
		t.Fatal("unsupported environment changed")
	}
}
