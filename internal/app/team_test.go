package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git-task/internal/git"
	"git-task/internal/storage"
	"git-task/internal/task"
	"git-task/internal/testrepo"
)

type teamFixture struct {
	a, b   *Plans
	ca, cb *git.Client
	remote string
	id     string
}

func teamNetwork(t *testing.T, c *git.Client, args ...string) []byte {
	t.Helper()
	r, err := c.RunNetwork(context.Background(), args...)
	if err != nil {
		t.Fatal(err)
	}
	return r.Stdout
}

func newTeamFixture(t *testing.T) teamFixture {
	t.Helper()
	ctx := context.Background()
	seed := testrepo.New(t)
	testrepo.Commit(t, seed)
	remote := filepath.Join(t.TempDir(), "remote.git")
	testrepo.Run(t, seed, "init", "--bare", "--initial-branch=main", remote)
	testrepo.Run(t, seed, "remote", "add", "origin", remote)
	teamNetwork(t, seed, "push", "origin", "refs/heads/main:refs/heads/main")
	openClone := func(author string) (*Plans, *git.Client) {
		root := filepath.Join(t.TempDir(), "clone "+author)
		teamNetwork(t, seed, "clone", "--no-local", "--", remote, root)
		c, err := git.New(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range seed.Env {
			if strings.HasPrefix(entry, "GIT_AUTHOR_NAME=") || strings.HasPrefix(entry, "GIT_COMMITTER_NAME=") {
				continue
			}
			c.Env = append(c.Env, entry)
		}
		c.Env = append(c.Env, "GIT_AUTHOR_NAME="+author, "GIT_COMMITTER_NAME="+author)
		testrepo.Run(t, c, "config", "core.hooksPath", filepath.Join(root, ".git", "hooks"))
		repo, err := c.Discover(ctx)
		if err != nil {
			t.Fatal(err)
		}
		p := &Plans{git: c, store: storage.New(root, repo.ExcludePath, c)}
		if _, _, err := p.Init(ctx, "main"); err != nil {
			t.Fatal(err)
		}
		return p, c
	}
	a, ca := openClone("Alice")
	b, cb := openClone("Bob")
	item, err := a.Add(ctx, "Shared task", "", task.Position{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Connect(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	if err := b.Connect(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	return teamFixture{a: a, b: b, ca: ca, cb: cb, remote: remote, id: item.ID}
}

func TestTeamTwoRealClonesOfflineStartsAndAllDone(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	beforeA := testrepo.Run(t, f.ca, "rev-parse", "refs/heads/main")
	beforeB := testrepo.Run(t, f.cb, "rev-parse", "refs/heads/main")
	a, err := f.a.Start(ctx, StartOptions{Branch: "alice", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.b.Start(ctx, StartOptions{Branch: "bob", ID: f.id})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.a.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	item, _, err := f.a.Show(ctx, f.id)
	if err != nil || len(item.Attempts) != 2 || item.Status != task.Active {
		t.Fatalf("parallel approaches: %+v %v", item, err)
	}
	if item.Attempts[0].Author == item.Attempts[1].Author || item.Attempts[0].ID == item.Attempts[1].ID {
		t.Fatal("identity lost")
	}
	preview, err := f.a.PrepareComplete(ctx, f.id, "", a.ActiveAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err = f.a.ApplyComplete(ctx, preview)
	if err != nil || item.Status != task.Active {
		t.Fatalf("first completion: %+v %v", item, err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	preview, err = f.b.PrepareComplete(ctx, f.id, "", b.ActiveAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.ApplyComplete(ctx, preview); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.a.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	item, _, err = f.a.Show(ctx, f.id)
	if err != nil || item.Status != task.Done || len(item.Attempts) != 2 {
		t.Fatalf("all done: %+v %v", item, err)
	}
	for _, p := range []*Plans{f.a, f.b} {
		s, err := p.store.Load(ctx)
		if err != nil || len(s.Plan.Team.Pending) != 0 {
			t.Fatalf("pending: %+v %v", s.Plan.Team, err)
		}
	}
	if !bytes.Equal(beforeA, testrepo.Run(t, f.ca, "rev-parse", "refs/heads/main")) || !bytes.Equal(beforeB, testrepo.Run(t, f.cb, "rev-parse", "refs/heads/main")) {
		t.Fatal("code history changed")
	}
	for _, c := range []*git.Client{f.ca, f.cb} {
		if len(testrepo.Run(t, c, "ls-files", "--", ".git-task")) != 0 {
			t.Fatal("local metadata tracked")
		}
		testrepo.Run(t, c, "check-ignore", ".git-task/plan.json")
		data := teamNetwork(t, c, "show", "refs/heads/git-task-plan:plan.json")
		if _, err := task.ValidateShared(data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTeamPublicationRaceRetryAndLostAcknowledgement(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Start(ctx, StartOptions{Branch: "alice", ID: f.id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.Start(ctx, StartOptions{Branch: "bob", ID: f.id}); err != nil {
		t.Fatal(err)
	}
	injected := false
	f.a.checkpoint = func(point string) error {
		if point == "publish-before-push" && !injected {
			injected = true
			return f.b.Publish(ctx)
		}
		return nil
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if !injected {
		t.Fatal("race was not injected")
	}
	f.a.checkpoint = nil
	if _, err := f.a.Add(ctx, "Offline addition", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	lost := errors.New("lost acknowledgement")
	f.a.checkpoint = func(point string) error {
		if point == "publish-after-push" {
			return lost
		}
		return nil
	}
	if err := f.a.Publish(ctx); !errors.Is(err, lost) {
		t.Fatalf("lost ack: %v", err)
	}
	base, err := f.a.store.Load(ctx)
	if err != nil || len(base.Plan.Team.Pending) != 1 {
		t.Fatalf("lost pending: %v", err)
	}
	id := base.Plan.Team.Pending[0].ID
	f.a.checkpoint = nil
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || len(after.Plan.Team.Pending) != 0 || len(after.Plan.Tasks) != 2 {
		t.Fatalf("retry: %+v %v", after.Plan, err)
	}
	count := 0
	for _, r := range after.Plan.Actions {
		if r.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("receipt delivered %d times", count)
	}
	if item, _ := after.Plan.FindID(f.id); len(item.Attempts) != 2 {
		t.Fatal("retry duplicated start")
	}
}

func TestTeamRepeatedRacesAreBoundedAndQueueSurvives(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	initial, err := f.b.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.cb.FetchPlan(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}
	commits := []string{first}
	for i := 0; i < 3; i++ {
		if _, err := f.b.Add(ctx, "Prepared competing addition", "", task.Position{}); err != nil {
			t.Fatal(err)
		}
		s, err := f.b.store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		next, err := task.Replay(initial.Plan, s.Plan.Team.Pending)
		if err != nil {
			t.Fatal(err)
		}
		data, err := task.SharedBytes(next)
		if err != nil {
			t.Fatal(err)
		}
		commit, err := f.cb.PlanCommit(ctx, commits[len(commits)-1], data)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, commit)
	}
	teamNetwork(t, f.cb, "push", "origin", commits[3]+":refs/heads/prepared-race")
	server, err := git.New(f.remote)
	if err != nil {
		t.Fatal(err)
	}
	server.Env = append([]string(nil), f.cb.Env...)
	if _, err := f.a.Add(ctx, "Local addition", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	count := 0
	f.a.checkpoint = func(point string) error {
		if point != "publish-before-push" {
			return nil
		}
		count++
		if count > 3 {
			return errors.New("unexpected fourth race")
		}
		_, err := server.Run(ctx, "update-ref", "refs/heads/git-task-plan", commits[count], commits[count-1])
		return err
	}
	if err := f.a.Publish(ctx); !errors.Is(err, git.ErrPushRace) || count != 3 {
		t.Fatalf("bound: %d %v", count, err)
	}
	s, err := f.a.store.Load(ctx)
	if err != nil || len(s.Plan.Team.Pending) != 1 {
		t.Fatalf("queue lost: %v", err)
	}
	f.a.checkpoint = nil
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	s, err = f.a.store.Load(ctx)
	if err != nil || len(s.Plan.Tasks) != 5 || len(s.Plan.Team.Pending) != 0 {
		t.Fatalf("next publication: %+v %v", s.Plan, err)
	}
}

func TestTeamOfflineAndConflictingVersionsStayReadable(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	before, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Add(ctx, "Offline addition", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	testrepo.Run(t, f.ca, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	if err := f.a.Publish(ctx); err == nil {
		t.Fatal("missing remote succeeded")
	}
	local, err := f.a.store.Load(ctx)
	if err != nil || len(local.Plan.Team.Pending) != 1 || len(local.Plan.Tasks) != len(before.Plan.Tasks)+1 {
		t.Fatal("offline state lost")
	}
	testrepo.Run(t, f.ca, "remote", "set-url", "origin", f.remote)
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.b.FetchTeam(ctx); err != nil {
		t.Fatal(err)
	}
	left, right := "Title left", "Title right"
	for _, entry := range []struct {
		p     *Plans
		title *string
	}{{f.a, &left}, {f.b, &right}} {
		s, err := entry.p.store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		next := s.Plan
		if _, err := next.Edit(f.id, task.EditOptions{Title: entry.title}); err != nil {
			t.Fatal(err)
		}
		if _, err := entry.p.store.Save(ctx, s, next); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.b.Publish(ctx); !errors.Is(err, task.ErrSharedConflict) {
		t.Fatalf("title conflict: %v", err)
	}
	s, err := f.b.store.Load(ctx)
	if err != nil || len(s.Plan.Team.Pending) != 1 {
		t.Fatalf("conflict unreadable: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(f.cb.Dir, ".git-task", "conflict-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatal("remote version not saved")
	}
	item, _ := s.Plan.FindID(f.id)
	if item.Title != right {
		t.Fatal("local conflict overwritten")
	}
}

func TestTeamCachedReceiptCannotRollBackSuccessfulPush(t *testing.T) {
	f := newTeamFixture(t)
	ctx := context.Background()
	if _, err := f.a.Add(ctx, "New task", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := f.a.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.ReceiveCached(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := f.a.store.Load(ctx)
	if err != nil || !slices.Equal(s.Plan.Order, after.Plan.Order) {
		t.Fatalf("old cached reference rolled back push: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.ca.Dir, ".git-task", "plan.json")); err != nil {
		t.Fatal(err)
	}
}
