package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func legacyTeamJSON(t *testing.T, data []byte, variant string) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if variant != "absent" {
		tail := ""
		if variant == "known" {
			var order []string
			if err := json.Unmarshal(fields["order"], &order); err != nil || len(order) == 0 {
				t.Fatalf("legacy order: %v", err)
			}
			tail = order[0]
		}
		fields["insertion_tail"], _ = json.Marshal(tail)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLegacyTeamQueueOfflineAndPublication(t *testing.T) {
	for _, variant := range []string{"absent", "empty", "known"} {
		t.Run(variant, func(t *testing.T) {
			f := newTeamFixture(t)
			ctx := context.Background()
			started, err := f.a.Start(ctx, StartOptions{Branch: "legacy-completed", ID: f.id})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := f.a.PrepareComplete(ctx, started.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			completed, err := f.a.ApplyComplete(ctx, preview)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.a.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			base, err := f.a.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			legacyShared := legacyTeamJSON(t, base.Plan.Team.Base, variant)
			commit, err := f.ca.PlanCommit(ctx, base.Plan.Team.BaseCommit, legacyShared)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.ca.PushPlan(ctx, "origin", base.Plan.Team.BaseCommit, commit); err != nil {
				t.Fatal(err)
			}
			if _, err := f.a.Add(ctx, "Legacy pending task", "", task.Position{}); err != nil {
				t.Fatal(err)
			}
			local, err := f.a.store.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			plan := local.Plan
			plan.Team.Base, plan.Team.BaseCommit = legacyShared, commit
			for i := range plan.Team.Pending {
				action := &plan.Team.Pending[i]
				action.Before = legacyTeamJSON(t, action.Before, variant)
				action.After = legacyTeamJSON(t, action.After, variant)
			}
			oldAction := plan.Team.Pending[0]
			oldDigest := oldAction.Digest()
			data, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			data = legacyTeamJSON(t, data, variant)
			path := filepath.Join(f.ca.Dir, ".git-task", "plan.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.ca.Dir, ".git-task", "plan.backup.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if result, _, err := f.a.InitTemplate(ctx, true); err != nil || result.Applied || !bytes.Equal(data, planBytes(t, f.ca)) {
				t.Fatalf("legacy apply no-op: %+v %v", result, err)
			}
			if _, err := f.a.StatusReport(ctx); err != nil || !bytes.Equal(data, planBytes(t, f.ca)) {
				t.Fatalf("legacy status no-op: %v", err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if err := f.a.Publish(canceled); !errors.Is(err, context.Canceled) || !bytes.Equal(data, planBytes(t, f.ca)) {
				t.Fatalf("legacy offline: %v", err)
			}
			added, err := f.a.Add(ctx, "New task", "", task.Position{})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := f.a.store.Load(ctx)
			if err != nil || saved.Plan.Order[len(saved.Plan.Order)-1] != added.ID || len(saved.Plan.Team.Pending) != 2 || saved.Plan.Team.Pending[0].Digest() != oldDigest {
				t.Fatalf("legacy mutation: %+v %v", saved.Plan, err)
			}
			if !reflect.DeepEqual(oldAction, saved.Plan.Team.Pending[0]) {
				var oldBefore, savedBefore, oldAfter, savedAfter any
				_ = json.Unmarshal(oldAction.Before, &oldBefore)
				_ = json.Unmarshal(saved.Plan.Team.Pending[0].Before, &savedBefore)
				_ = json.Unmarshal(oldAction.After, &oldAfter)
				_ = json.Unmarshal(saved.Plan.Team.Pending[0].After, &savedAfter)
				if !reflect.DeepEqual(oldBefore, savedBefore) || !reflect.DeepEqual(oldAfter, savedAfter) {
					t.Fatal("legacy action snapshots changed")
				}
			}
			var root map[string]json.RawMessage
			if err := json.Unmarshal(planBytes(t, f.ca), &root); err != nil || root["insertion_tail"] != nil {
				t.Fatalf("new root emitted legacy cursor: %v", err)
			}
			for _, snapshot := range []json.RawMessage{saved.Plan.Team.Pending[1].Before, saved.Plan.Team.Pending[1].After} {
				if bytes.Contains(snapshot, []byte("insertion_tail")) {
					t.Fatal("new action emitted legacy cursor")
				}
			}
			lost := errors.New("lost acknowledgement")
			f.a.checkpoint = func(point string) error {
				if point == "publish-after-push" {
					return lost
				}
				return nil
			}
			if err := f.a.Publish(ctx); !errors.Is(err, lost) {
				t.Fatalf("legacy push lost acknowledgement: %v", err)
			}
			f.a.checkpoint = nil
			if err := f.a.Publish(ctx); err != nil {
				t.Fatal(err)
			}
			if err := f.b.FetchTeam(ctx); err != nil {
				t.Fatal(err)
			}
			received, err := f.b.store.Load(ctx)
			if err != nil || len(received.Plan.Tasks) != 3 {
				t.Fatalf("legacy delivery: %+v %v", received.Plan, err)
			}
			preserved, err := received.Plan.FindID(completed.ID)
			if err != nil || preserved.Status != task.Done || !reflect.DeepEqual(preserved.Attempts, task.Shared(task.Plan{Tasks: []task.Task{completed}}).Tasks[0].Attempts) {
				t.Fatalf("legacy completion history lost: %+v %v", preserved, err)
			}
			count := 0
			for _, receipt := range received.Plan.Actions {
				if receipt.ID == oldAction.ID {
					count++
					if receipt.Digest != oldDigest {
						t.Fatal("legacy receipt digest changed")
					}
				}
			}
			if count != 1 {
				t.Fatalf("legacy receipt count: %d", count)
			}
		})
	}
}

func TestAppendAfterCompletionThroughApplication(t *testing.T) {
	for _, mode := range []string{"manual", "tracking"} {
		for _, command := range []string{"add", "start-new"} {
			t.Run(mode+"/"+command, func(t *testing.T) {
				p, c := startFixture(t)
				ctx := context.Background()
				started, err := p.Start(ctx, StartOptions{Branch: "completed-first"})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "manual" {
					preview, err := p.PrepareComplete(ctx, started.ID, "")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := p.ApplyComplete(ctx, preview); err != nil {
						t.Fatal(err)
					}
				} else {
					testrepo.Commit(t, c)
					if _, _, err := p.Sync(ctx); err != nil {
						t.Fatal(err)
					}
					testrepo.Run(t, c, "checkout", "main")
					testrepo.Run(t, c, "merge", "--ff-only", "completed-first")
					if _, _, err := p.Sync(ctx); err != nil {
						t.Fatal(err)
					}
				}
				before, err := p.store.Load(ctx)
				if err != nil {
					t.Fatal(err)
				}
				done, err := before.Plan.FindID(started.ID)
				if err != nil || done.Status != task.Done {
					t.Fatalf("completion: %+v %v", done, err)
				}
				var added task.Task
				if command == "add" {
					added, err = p.Add(ctx, "Next", "", task.Position{})
				} else {
					added, err = p.Start(ctx, StartOptions{Branch: "new-last", New: "Next"})
				}
				if err != nil {
					t.Fatal(err)
				}
				after, err := p.store.Load(ctx)
				if err != nil || !slices.Equal(after.Plan.Order, append(slices.Clone(before.Plan.Order), added.ID)) {
					t.Fatalf("append after completion: %v %v", after.Plan.Order, err)
				}
				preserved, err := after.Plan.FindID(started.ID)
				if err != nil || !reflect.DeepEqual(preserved.Attempts, done.Attempts) {
					t.Fatalf("completion history changed: %+v %v", preserved, err)
				}
			})
		}
	}
}
