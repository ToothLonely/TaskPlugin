package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestAcceptanceMigrationAllStatuses(t *testing.T) {
	f := newHookFixture(t, buildHooksBinary(t))
	head, index, refs := f.runGit("rev-parse", "HEAD"), f.runGit("ls-files", "--stage"), f.runGit("show-ref")
	legacy := []byte(strings.ReplaceAll(`{
"format":"git-task","schema_version":1,"revision":7,"target_branch":"main",
"order":["todo-id","active-id","paused-id","done-id","archived-id"],
"tasks":[
{"id":"todo-id","number":"T-001","title":"Todo","revision":5,"status":"todo"},
{"id":"active-id","number":"T-002","title":"Active","revision":5,"status":"active","active_attempt":{"id":"active-open","branch":"legacy-active","original_branch":"legacy-active","target_branch":"main","base_commit":"$BASE","started_at":"2026-01-01T00:00:00Z"}},
{"id":"paused-id","number":"T-003","title":"Paused","revision":5,"status":"paused","active_attempt":{"id":"paused-open","branch":"legacy-paused","original_branch":"legacy-paused","target_branch":"main","base_commit":"$BASE","started_at":"2026-01-01T00:00:00Z"},"attempts":[{"id":"paused-history","completion":{"event":1,"source":"manual","target_branch":"main"}}]},
{"id":"done-id","number":"T-004","title":"Done","revision":5,"status":"done","attempts":[{"id":"done-history","completion":{"event":2,"source":"imported","target_branch":"main"}}]},
{"id":"archived-id","number":"T-005","title":"Archived","revision":5,"status":"archived","active_attempt":{"id":"archived-open","branch":"legacy-archived","original_branch":"legacy-archived","target_branch":"main","base_commit":"$BASE","started_at":"2026-01-01T00:00:00Z"},"attempts":[{"id":"archived-history","completion":{"event":3,"source":"manual","target_branch":"main"}}]}
],"last_event":3,"insertion_tail":"archived-id"}`, "$BASE", strings.TrimSpace(string(head))))
	dir := filepath.Join(f.git.Dir, ".git-task")
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	f.cli("migrate")
	p := acceptancePlan(t, f.planBytes())
	order := []string{"todo-id", "active-id", "paused-id", "done-id", "archived-id"}
	if p.SchemaVersion != 2 || p.Revision != 7 || p.TargetBranch != "main" || p.LastEvent != 3 || p.InsertionTail != "archived-id" || !reflect.DeepEqual(p.Order, order) || len(p.Tasks) != 5 {
		t.Fatalf("migration changed plan identity/history: %+v", p)
	}
	for _, expected := range []struct {
		id      string
		status  task.Status
		ids     []string
		states  []task.Status
		sources []task.Source
	}{
		{"todo-id", task.Todo, nil, nil, nil},
		{"active-id", task.Active, []string{"active-open"}, []task.Status{task.Active}, []task.Source{""}},
		{"paused-id", task.Paused, []string{"paused-history", "paused-open"}, []task.Status{task.Done, task.Paused}, []task.Source{task.Manual, ""}},
		{"done-id", task.Done, []string{"done-history"}, []task.Status{task.Done}, []task.Source{task.Imported}},
		{"archived-id", task.Archived, []string{"archived-history", "archived-open"}, []task.Status{task.Done, task.Active}, []task.Source{task.Manual, ""}},
	} {
		item, err := p.FindID(expected.id)
		if err != nil || item.Status != expected.status || item.Revision != 5 || len(item.Attempts) != len(expected.ids) {
			t.Fatalf("migration %s: %+v %v", expected.id, item, err)
		}
		for i, attempt := range item.Attempts {
			if attempt.ID != expected.ids[i] || attempt.Status != expected.states[i] || attempt.Author != "" {
				t.Fatalf("approach identity/status/author changed: %+v", attempt)
			}
			if expected.sources[i] == "" {
				if attempt.Completion != nil || attempt.BaseCommit != strings.TrimSpace(string(head)) || attempt.StartedAt == nil {
					t.Fatalf("open approach evidence changed: %+v", attempt)
				}
			} else if attempt.Completion == nil || attempt.Completion.Source != expected.sources[i] {
				t.Fatalf("completion lost: %+v", attempt)
			} else if event := map[string]uint64{"paused-history": 1, "done-history": 2, "archived-history": 3}[attempt.ID]; attempt.Completion.Event != event {
				t.Fatalf("completion event changed: %+v", attempt)
			}
		}
	}
	before := f.planBytes()
	f.cli("migrate")
	for _, name := range []string{"plan.schema-1.json", "plan.backup.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(data, legacy) {
			t.Fatalf("original schema-1 bytes lost: %s %v", name, err)
		}
	}
	if !bytes.Equal(before, f.planBytes()) || !bytes.Equal(head, f.runGit("rev-parse", "HEAD")) || !bytes.Equal(index, f.runGit("ls-files", "--stage")) || !bytes.Equal(refs, f.runGit("show-ref")) {
		t.Fatal("migration/repeat changed plan, refs, HEAD or index")
	}
}

func TestAcceptanceTeamJSONTransfer(t *testing.T) {
	binary := buildHooksBinary(t)
	a, b := teamHookPair(t, binary)
	a, b = acceptanceAuthor(a, "Alice"), acceptanceAuthor(b, "Bob")
	a.cli("start", "alice-history", "--id", "task-001")
	a.cli("complete", "--id", "task-001", "--yes")
	a.cli("start", "alice", "--id", "task-001", "--again")
	b.cli("start", "bob", "--id", "task-001")
	a.cli("team", "fetch")
	a.write("alice.txt", "own work\n")
	a.commit("Alice work")
	a.cli("sync")
	source := acceptancePlan(t, a.planBytes())
	if len(source.Tasks[0].Attempts) != 3 || source.Tasks[0].Attempts[0].Completion == nil || source.Team == nil || len(source.Team.LocalAttempts) == 0 {
		t.Fatal("source lacks parallel approaches and local assignment")
	}
	observed := false
	for _, attempt := range source.Tasks[0].Attempts {
		observed = observed || attempt.Observation != nil
	}
	if !observed {
		t.Fatal("source lacks an observation to test export cleanup")
	}
	path := filepath.Join(t.TempDir(), "team snapshot.json")
	a.cli("export", "--format=json", "--output", path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exported := acceptancePlan(t, data)
	assertAcceptanceTransfer(t, source, exported)
	destination := testrepo.New(t)
	code := []byte("destination code\n")
	if err := os.WriteFile(filepath.Join(destination.Dir, "keep.txt"), code, 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, destination, "add", "--", "keep.txt")
	testrepo.Commit(t, destination)
	f := hookFixture{t: t, ctx: a.ctx, git: destination, binary: binary}
	f.cli("init", "--target", "release")
	head, index, refs := f.runGit("rev-parse", "HEAD"), f.runGit("ls-files", "--stage"), f.runGit("show-ref")
	config := f.runGit("config", "--local", "--list")
	hookPath := strings.TrimSpace(string(f.runGit("config", "--get", "core.hooksPath")))
	files, err := os.ReadDir(hookPath)
	if err != nil || len(files) != 0 {
		t.Fatalf("destination must have no installed hooks: %v", err)
	}
	f.cli("import", path, "--format=json", "--yes")
	imported := acceptancePlan(t, f.planBytes())
	assertAcceptanceTransfer(t, source, imported)
	roundtripPath := filepath.Join(t.TempDir(), "roundtrip.json")
	f.cli("export", "--format=json", "--output", roundtripPath)
	roundtrip, err := os.ReadFile(roundtripPath)
	if err != nil || !bytes.Equal(data, roundtrip) {
		t.Fatalf("team JSON round-trip changed shared snapshot: %v", err)
	}
	if !bytes.Equal(head, f.runGit("rev-parse", "HEAD")) || !bytes.Equal(index, f.runGit("ls-files", "--stage")) || !bytes.Equal(refs, f.runGit("show-ref")) {
		t.Fatal("JSON import created refs or changed code history/index")
	}
	if len(f.runGit("remote")) != 0 {
		t.Fatal("JSON import invented remote configuration")
	}
	files, err = os.ReadDir(hookPath)
	if err != nil || len(files) != 0 || !bytes.Equal(config, f.runGit("config", "--local", "--list")) {
		t.Fatal("JSON import installed hooks or changed local Git configuration")
	}
	kept, err := os.ReadFile(filepath.Join(destination.Dir, "keep.txt"))
	if err != nil || !bytes.Equal(kept, code) {
		t.Fatal("JSON import changed destination code")
	}
}

func assertAcceptanceTransfer(t *testing.T, source, transferred task.Plan) {
	t.Helper()
	if transferred.Team != nil || len(transferred.ServerEvents) != 0 || transferred.TargetBranch != source.TargetBranch || !reflect.DeepEqual(transferred.Order, source.Order) || transferred.InsertionTail != source.InsertionTail || transferred.LastEvent != source.LastEvent || !reflect.DeepEqual(transferred.Actions, source.Actions) || len(transferred.Tasks) != len(source.Tasks) {
		t.Fatalf("JSON transfer changed shared identity or copied local settings: %+v", transferred)
	}
	for i, original := range source.Tasks {
		item := transferred.Tasks[i]
		if item.ID != original.ID || item.Number != original.Number || item.Title != original.Title || item.Description != original.Description || item.Revision != original.Revision || item.Status != original.Status || len(item.Attempts) != len(original.Attempts) || len(item.Warnings) != 0 || item.ActiveAttempt != nil {
			t.Fatalf("JSON task transfer changed identity/status: %+v", item)
		}
		for j, original := range original.Attempts {
			attempt := item.Attempts[j]
			original.Observation = nil
			if attempt.Author == "" || !reflect.DeepEqual(attempt, original) {
				t.Fatalf("JSON approach transfer changed history or copied observation: %+v", attempt)
			}
		}
	}
}

func TestAcceptanceUninstallPreservesTeamQueue(t *testing.T) {
	a, b := teamHookPair(t, buildHooksBinary(t))
	foreign := []byte("#!/bin/sh\nprintf 'foreign\\n' >> foreign-after-uninstall\n")
	if err := os.WriteFile(filepath.Join(a.dir, "post-commit"), foreign, 0755); err != nil {
		t.Fatal(err)
	}
	a.cli("hooks", "install")
	remote := strings.TrimSpace(string(a.runGit("remote", "get-url", "origin")))
	a.runGit("remote", "set-url", "origin", filepath.Join(t.TempDir(), "offline.git"))
	a.cli("start", "queued-at-uninstall", "--id", "task-001")
	a.cli("add", "Offline queued addition")
	before := a.planBytes()
	snapshot := acceptancePlan(t, before)
	if snapshot.Team == nil || len(snapshot.Team.Pending) != 2 || len(snapshot.Team.LocalAttempts) != 1 {
		t.Fatal("uninstall source lacks queue and local assignment")
	}
	head, index, refs := a.runGit("rev-parse", "HEAD"), a.runGit("ls-files", "--stage"), a.runGit("show-ref")
	a.cli("doctor")
	a.cli("hooks", "uninstall")
	if !bytes.Equal(before, a.planBytes()) || !bytes.Equal(head, a.runGit("rev-parse", "HEAD")) || !bytes.Equal(index, a.runGit("ls-files", "--stage")) || !bytes.Equal(refs, a.runGit("show-ref")) {
		t.Fatal("doctor/uninstall changed plan, queue, assignment, code or refs")
	}
	data, err := os.ReadFile(filepath.Join(a.dir, "post-commit"))
	if err != nil || !bytes.Equal(data, foreign) {
		t.Fatal("uninstall did not restore foreign hook exactly")
	}
	a.commit("foreign handler after uninstall")
	data, err = os.ReadFile(filepath.Join(a.git.Dir, "foreign-after-uninstall"))
	if err != nil || string(data) != "foreign\n" || !bytes.Equal(before, a.planBytes()) {
		t.Fatal("foreign hook missing or own handler still mutated queued plan")
	}
	if err := os.Remove(filepath.Join(a.git.Dir, "foreign-after-uninstall")); err != nil {
		t.Fatal(err)
	}
	a.runGit("remote", "set-url", "origin", remote)
	a.cli("team", "publish")
	b.cli("team", "fetch")
	for _, f := range []hookFixture{a, b} {
		p := acceptancePlan(t, f.planBytes())
		if len(p.Team.Pending) != 0 || len(p.Tasks) != 2 || len(f.saved().Attempts) != 1 || f.saved().Attempts[0].ID != snapshot.Tasks[0].Attempts[0].ID {
			t.Fatal("queue or attempt lost after uninstall/publish")
		}
		for _, action := range snapshot.Team.Pending {
			matches := 0
			for _, receipt := range p.Actions {
				if receipt.ID == action.ID && receipt.Digest == action.Digest() {
					matches++
				}
			}
			if matches != 1 {
				t.Fatal("queued action lost or duplicated after uninstall")
			}
		}
	}
}
