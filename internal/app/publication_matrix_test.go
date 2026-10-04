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

	"git-task/internal/task"
	"git-task/internal/testrepo"
)

func TestPublicationRepeatedRacesWithAuthorsAndServer(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	c, cc := f.clone("Charlie")
	if err := c.Connect(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	a, err := f.a.Start(ctx, StartOptions{Branch: "alice-server", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := f.b.Start(ctx, StartOptions{Branch: "bob-offline", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main")))
	codePath := filepath.Join(f.ca.Dir, "work.txt")
	if err := os.WriteFile(codePath, []byte("Alice work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, f.ca, "add", "--", "work.txt")
	testrepo.Commit(t, f.ca)
	teamNetwork(t, f.ca, "push", "origin", "refs/heads/alice-server:refs/heads/alice-server")
	testrepo.Run(t, f.ca, "checkout", "main")
	testrepo.Run(t, f.ca, "merge", "--ff-only", "alice-server")
	after := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main")))
	teamNetwork(t, f.ca, "push", "origin", "refs/heads/main:refs/heads/main")
	if _, err := f.a.Add(ctx, "Alice queued addition", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Add(ctx, "Charlie queued addition", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codePath, []byte("staged work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, f.ca, "add", "--", "work.txt")
	dirtyCode := []byte("unstaged work\n")
	if err := os.WriteFile(codePath, dirtyCode, 0600); err != nil {
		t.Fatal(err)
	}
	index := testrepo.Run(t, f.ca, "ls-files", "--stage")
	stages := 0
	f.a.checkpoint = func(point string) error {
		if point != "publish-before-push" {
			return nil
		}
		stages++
		switch stages {
		case 1:
			return f.b.Publish(ctx)
		case 2:
			if err := c.ReconcileTeam(ctx, before, after); err != nil {
				return err
			}
		}
		return nil
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if stages != 3 {
		t.Fatalf("expected two stale pushes and a successful third push, got %d", stages)
	}
	f.a.checkpoint = nil
	for _, p := range []*Plans{f.a, f.b, c} {
		if err := p.FetchTeam(ctx); err != nil {
			t.Fatal(err)
		}
		s, err := p.store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item, err := s.Plan.FindID(f.id)
		if err != nil {
			t.Fatal(err)
		}
		alice, err := item.Attempt(a.ActiveAttempt.ID)
		if err != nil {
			t.Fatal(err)
		}
		bob, err := item.Attempt(b.ActiveAttempt.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Plan.Tasks) != 3 || len(item.Attempts) != 2 || item.Status != task.Active || alice.Status != task.Done || bob.Status != task.Active || alice.Author != a.ActiveAttempt.Author || bob.Author != b.ActiveAttempt.Author || len(s.Plan.Team.Pending) != 0 {
			t.Fatalf("lost action, author, or completion: %+v", s.Plan)
		}
		ids := map[string]bool{}
		for _, receipt := range s.Plan.Actions {
			if ids[receipt.ID] {
				t.Fatalf("duplicate receipt: %s", receipt.ID)
			}
			ids[receipt.ID] = true
		}
	}
	if got := strings.TrimSpace(string(testrepo.Run(t, f.ca, "rev-parse", "main"))); got != after || !bytes.Equal(index, testrepo.Run(t, f.ca, "ls-files", "--stage")) {
		t.Fatal("publication changed code history or index")
	}
	if data, err := os.ReadFile(codePath); err != nil || !bytes.Equal(data, dirtyCode) {
		t.Fatalf("publication changed dirty code: %v", err)
	}
	remoteBefore := teamNetwork(t, cc, "ls-remote", "origin", "refs/heads/git-task-plan")
	if err := c.ReconcileTeam(ctx, before, after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(remoteBefore, teamNetwork(t, cc, "ls-remote", "origin", "refs/heads/git-task-plan")) {
		t.Fatal("repeated server event republished completed work")
	}
}

func TestPublicationCancellationAtPushBarrierRetainsIdentity(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	item, err := f.a.Start(ctx, StartOptions{Branch: "cancel-at-push", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	base, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	remoteBefore := teamNetwork(t, f.ca, "ls-remote", "origin", "refs/heads/git-task-plan")
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	reached := false
	f.a.checkpoint = func(point string) error {
		if point == "publish-before-push" {
			reached = true
			cancel()
		}
		return nil
	}
	if err := f.a.Publish(canceled); !errors.Is(err, context.Canceled) || !reached {
		t.Fatalf("barrier cancellation: %v, reached=%v", err, reached)
	}
	s, err := f.a.store.Load(ctx)
	if err != nil || !base.SameVersion(s) || !bytes.Equal(remoteBefore, teamNetwork(t, f.ca, "ls-remote", "origin", "refs/heads/git-task-plan")) {
		t.Fatalf("cancellation changed queue or remote: %v", err)
	}
	f.a.checkpoint = nil
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	s, err = f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Plan.FindID(f.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Plan.Team.Pending) != 0 || len(got.Attempts) != 1 || !reflect.DeepEqual(got.Attempts[0], item.Attempts[0]) {
		t.Fatalf("retry replaced approach: %+v", got)
	}
	count := 0
	for _, receipt := range s.Plan.Actions {
		if receipt.ID == base.Plan.Team.Pending[0].ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("original action delivered %d times", count)
	}
}
