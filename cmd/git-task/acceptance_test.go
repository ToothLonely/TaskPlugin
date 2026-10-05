package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"git-task/internal/git"
	"git-task/internal/task"
)

func acceptancePlan(t *testing.T, data []byte) task.Plan {
	t.Helper()
	var p task.Plan
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func acceptanceAuthor(f hookFixture, name string) hookFixture {
	var env []string
	for _, entry := range f.git.Env {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "GIT_AUTHOR_NAME") || strings.EqualFold(key, "GIT_AUTHOR_EMAIL") {
			continue
		}
		env = append(env, entry)
	}
	f.git.Env = append(env, "GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+name+"@example.invalid")
	return f
}

func TestAcceptanceOfflineApproachesAndLateDelivery(t *testing.T) {
	for _, order := range []string{"alice-first", "bob-first"} {
		t.Run(order, func(t *testing.T) {
			testAcceptanceOfflineApproaches(t, order == "bob-first")
		})
	}
}

func testAcceptanceOfflineApproaches(t *testing.T, reverse bool) {
	t.Helper()
	a, b := teamHookPair(t, buildHooksBinary(t))
	a, b = acceptanceAuthor(a, "Alice"), acceptanceAuthor(b, "Bob")
	remote := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	root := filepath.Join(t.TempDir(), "late clone")
	networkGit(t, a, "clone", "--", remote, root)
	c, err := git.New(root)
	if err != nil {
		t.Fatal(err)
	}
	c.Env = append([]string(nil), a.git.Env...)
	late := acceptanceAuthor(hookFixture{t: t, ctx: a.ctx, git: c, binary: a.binary}, "Charlie")
	late.cli("init")
	late.cli("team", "connect", "--remote", "origin")
	for i, f := range []hookFixture{a, b, late} {
		f.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
		out := f.cli("start", []string{"alice", "bob", "charlie"}[i], "--id", "task-001")
		if !strings.Contains(string(out), "active") {
			t.Fatalf("offline start: %s", out)
		}
		p := acceptancePlan(t, f.planBytes())
		if len(p.Team.Pending) == 0 || len(f.saved().Attempts) != 1 || f.saved().Attempts[0].Author == "" {
			t.Fatal("offline identity or queue missing")
		}
		f.runGit("remote", "set-url", "origin", remote)
	}
	aliceID := a.saved().Attempts[0].ID
	bobID := b.saved().Attempts[0].ID
	if aliceID == bobID {
		t.Fatal("offline approaches reused an ID")
	}
	if reverse {
		b.cli("team", "publish")
		a.cli("team", "publish")
	} else {
		a.cli("team", "publish")
		b.cli("team", "publish")
	}
	a.cli("team", "fetch")
	ids := map[string]string{}
	authors := map[string]bool{}
	for _, attempt := range a.saved().Attempts {
		ids[attempt.ID] = attempt.Author
		authors[attempt.Author] = true
	}
	if len(ids) != 2 || len(authors) != 2 || ids[aliceID] != "Alice <Alice@example.invalid>" || ids[bobID] != "Bob <Bob@example.invalid>" || a.saved().Status != task.Active {
		t.Fatal("independent approaches lost")
	}
	a.cli("complete", "--id", "task-001", "--attempt", aliceID, "--yes")
	b.cli("team", "fetch")
	if b.saved().Status != task.Active {
		t.Fatal("one completion closed the task")
	}
	var remaining string
	for _, attempt := range b.saved().Attempts {
		if attempt.Status != task.Done {
			remaining = attempt.ID
		}
	}
	b.cli("pause", "--id", "task-001", "--attempt", remaining)
	b.cli("complete", "--id", "task-001", "--attempt", remaining, "--yes")
	a.cli("team", "fetch")
	if a.saved().Status != task.Done {
		t.Fatal("all completed approaches did not close task")
	}
	lateID := late.saved().Attempts[0].ID
	late.cli("team", "publish")
	out := a.cli("team", "fetch")
	if a.saved().Status != task.Active || len(a.saved().Attempts) != 3 || !strings.Contains(string(out), "поздний подход") {
		t.Fatalf("late approach or notice missing: %s %+v", out, a.saved())
	}
	before := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	late.cli("team", "publish")
	if !bytes.Equal(before, networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")) {
		t.Fatal("repeat added metadata commit")
	}
	for _, f := range []hookFixture{a, b, late} {
		f.cli("team", "fetch")
		p := acceptancePlan(t, f.planBytes())
		item := f.saved()
		if item.Status != task.Active || len(item.Attempts) != 3 || len(p.Team.Pending) != 0 {
			t.Fatalf("copies diverged: %+v", p)
		}
		for _, attempt := range item.Attempts {
			if attempt.ID == lateID {
				if attempt.Status != task.Active {
					t.Fatal("late approach closed")
				}
			} else if ids[attempt.ID] != attempt.Author || attempt.Status != task.Done {
				t.Fatal("original history changed")
			}
		}
	}
	networkGit(t, a, "fetch", "origin", "refs/heads/git-task-plan")
	shared, err := task.ValidateShared(a.runGit("show", "FETCH_HEAD:plan.json"))
	if err != nil || len(shared.Tasks[0].Attempts) != 3 || shared.Tasks[0].Status != task.Active {
		t.Fatalf("remote diverged: %+v %v", shared, err)
	}
}

func TestAcceptancePublicationBarriers(t *testing.T) {
	proxy := buildAcceptanceGit(t)
	for _, mode := range []string{"retry", "exhaustion", "lost-ack"} {
		t.Run(mode, func(t *testing.T) {
			a, b := teamHookPair(t, buildHooksBinary(t))
			head := a.runGit("rev-parse", "HEAD")
			a.write("base.txt", "staged\n")
			if err := os.WriteFile(filepath.Join(a.git.Dir, "base.txt"), []byte("unstaged\n"), 0600); err != nil {
				t.Fatal(err)
			}
			index := a.runGit("ls-files", "--stage")
			remote := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
			a.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
			a.cli("add", "Queued Alice")
			a.runGit("remote", "set-url", "origin", remote)
			pending := acceptancePlan(t, a.planBytes()).Team.Pending
			marker := filepath.Join(t.TempDir(), "barrier-count")
			originalEnv := a.git.Env
			a.git.Env = acceptanceProxyEnv(t, originalEnv, proxy, a.binary, b.git.Dir, marker, mode)
			out, code := a.command("team", "publish")
			if mode == "exhaustion" || mode == "lost-ack" {
				if code == 0 || !reflect.DeepEqual(pending, acceptancePlan(t, a.planBytes()).Team.Pending) {
					t.Fatalf("limit lost queue: %d %s", code, out)
				}
			} else if code != 0 {
				t.Fatalf("publication: %d %s", code, out)
			}
			count, err := os.ReadFile(marker)
			want := map[string]string{"retry": "2\n", "exhaustion": "3\n", "lost-ack": "1\n"}[mode]
			if err != nil || string(count) != want {
				t.Fatalf("push barrier count=%q want=%q: %v; publish code=%d output=%s", count, want, err, code, out)
			}
			a.git.Env = originalEnv
			a.cli("team", "publish")
			for _, f := range []hookFixture{a, b} {
				f.cli("team", "fetch")
				p := acceptancePlan(t, f.planBytes())
				if len(p.Team.Pending) != 0 {
					t.Fatal("queue not delivered")
				}
				for _, action := range pending {
					matches := 0
					for _, receipt := range p.Actions {
						if receipt.ID == action.ID && receipt.Digest == action.Digest() {
							matches++
						}
					}
					if matches != 1 {
						t.Fatalf("receipt duplicated or lost: %s (%d)", action.ID, matches)
					}
				}
				wantTasks := map[string]int{"retry": 3, "exhaustion": 5, "lost-ack": 2}[mode]
				if len(p.Tasks) != wantTasks {
					t.Fatalf("concurrent action lost: %d want %d", len(p.Tasks), wantTasks)
				}
			}
			networkGit(t, a, "fetch", "origin", "refs/heads/git-task-plan")
			shared, err := task.ValidateShared(a.runGit("show", "FETCH_HEAD:plan.json"))
			if err != nil || !reflect.DeepEqual(shared.Tasks, acceptancePlan(t, b.planBytes()).Tasks) {
				t.Fatalf("remote differs: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(a.git.Dir, "base.txt"))
			if err != nil || string(data) != "unstaged\n" || !bytes.Equal(head, a.runGit("rev-parse", "HEAD")) || !bytes.Equal(index, a.runGit("ls-files", "--stage")) {
				t.Fatal("publication changed code, HEAD or index")
			}
		})
	}
}

func buildAcceptanceGit(t *testing.T) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	path := filepath.Join(t.TempDir(), "git"+suffix)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", path, "../../scripts/acceptance-git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Git barrier: %v %s", err, out)
	}
	return path
}

func acceptanceProxyEnv(t *testing.T, original []string, proxy, binary, other, counter, mode string) []string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	var path string
	for _, entry := range original {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PATH") {
			path = value
			continue
		}
		env = append(env, entry)
	}
	return append(env, "PATH="+filepath.Dir(proxy)+string(os.PathListSeparator)+path,
		"TASK_ACCEPTANCE_PATH="+path, "TASK_ACCEPTANCE_REAL_GIT="+realGit,
		"TASK_ACCEPTANCE_BINARY="+binary, "TASK_ACCEPTANCE_OTHER="+other,
		"TASK_ACCEPTANCE_COUNTER="+counter, "TASK_ACCEPTANCE_MODE="+mode)
}

func TestAcceptanceCanceledPublicationKeepsQueue(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	remote := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	a.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
	a.cli("add", "Cancel at push")
	a.runGit("remote", "set-url", "origin", remote)
	before := a.planBytes()
	remoteBefore := networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	env := acceptanceProxyEnv(t, a.git.Env, buildAcceptanceGit(t), a.binary, b.git.Dir, filepath.Join(t.TempDir(), "count"), "cancel")
	env = append(env, "TASK_ACCEPTANCE_BARRIER="+listener.Addr().String())
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.binary, "team", "publish")
	cmd.Dir, cmd.Env, cmd.WaitDelay = a.git.Dir, env, 2*time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		cancel()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	cancel()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("canceled command succeeded: %s", &output)
	}
	if !bytes.Equal(before, a.planBytes()) || !bytes.Equal(remoteBefore, networkGit(t, a, "ls-remote", "origin", "refs/heads/git-task-plan")) {
		t.Fatal("cancellation changed queue or remote")
	}
	a.cli("team", "publish")
	b.cli("team", "fetch")
	if len(acceptancePlan(t, a.planBytes()).Team.Pending) != 0 || len(acceptancePlan(t, b.planBytes()).Tasks) != 2 {
		t.Fatal("canceled action not delivered later")
	}
}

func TestAcceptanceMigrationAndBackupRepair(t *testing.T) {
	f := newHookFixture(t, buildHooksBinary(t))
	head := f.runGit("rev-parse", "HEAD")
	index := f.runGit("ls-files", "--stage")
	legacy := []byte(`{"format":"git-task","schema_version":1,"revision":7,"target_branch":"main","order":["legacy-task"],"tasks":[{"id":"legacy-task","number":"T-007","title":"Legacy","revision":5,"status":"paused","active_attempt":{"id":"active-id","branch":"feature","original_branch":"feature","target_branch":"main","base_commit":"` + strings.TrimSpace(string(head)) + `","started_at":"2026-01-01T00:00:00Z"},"attempts":[{"id":"historic-id","completion":{"event":1,"source":"manual","target_branch":"main"}}]}],"last_event":1,"insertion_tail":"legacy-task"}`)
	dir := filepath.Join(f.git.Dir, ".git-task")
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	f.cli("migrate")
	p := acceptancePlan(t, f.planBytes())
	item, err := p.FindID("legacy-task")
	if err != nil || item.Status != task.Paused || len(item.Attempts) != 2 || item.Attempts[0].ID != "historic-id" || item.Attempts[1].ID != "active-id" {
		t.Fatalf("migration lost history: %+v %v", item, err)
	}
	for _, attempt := range item.Attempts {
		if attempt.Author != "" {
			t.Fatal("migration fabricated author")
		}
	}
	for _, name := range []string{"plan.schema-1.json", "plan.backup.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(data, legacy) {
			t.Fatalf("legacy source lost: %s %v", name, err)
		}
	}
	before := f.planBytes()
	f.cli("migrate")
	if !bytes.Equal(before, f.planBytes()) {
		t.Fatal("repeat migration rewrote plan")
	}
	f.cli("edit", "--id", "legacy-task", "--title", "Changed")
	backup, err := os.ReadFile(filepath.Join(dir, "plan.backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	damaged := []byte("{broken acceptance fixture\n")
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, code := f.command("doctor"); code == 0 || !bytes.Equal(damaged, f.planBytes()) {
		t.Fatal("doctor changed or accepted damaged plan")
	}
	f.cli("doctor", "--repair", "restore-backup", "--yes")
	if !bytes.Equal(backup, f.planBytes()) {
		t.Fatal("backup not restored exactly")
	}
	files, err := filepath.Glob(filepath.Join(dir, "conflict-original-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("original not preserved: %v %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || !bytes.Equal(data, damaged) {
		t.Fatal("damaged source lost")
	}
	f.cli("doctor", "--repair", "recover-start", "--yes")
	if !bytes.Equal(head, f.runGit("rev-parse", "HEAD")) || !bytes.Equal(index, f.runGit("ls-files", "--stage")) || !bytes.Equal(backup, f.planBytes()) {
		t.Fatal("repair changed code or repeated a mutation")
	}
}
