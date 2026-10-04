package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-task/internal/app"
	"git-task/internal/testrepo"
)

func TestCancelledCompleteDoesNotPublishAnotherProcessAction(t *testing.T) {
	p, c := selectionFixture(t)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	testrepo.Run(t, c, "init", "--bare", "--initial-branch=main", remote)
	testrepo.Run(t, c, "remote", "add", "origin", remote)
	network := func(args ...string) []byte {
		t.Helper()
		r, err := c.RunNetwork(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return r.Stdout
	}
	network("push", "origin", "refs/heads/main:refs/heads/main")
	if err := p.Connect(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	remoteBefore := network("ls-remote", "origin", "refs/heads/git-task-plan")
	var planAfterWriter []byte
	dialogue := &scriptedDialogue{
		reader: bufio.NewReader(strings.NewReader("no\n")),
		before: func() {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			writerCtx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			cmd := exec.CommandContext(writerCtx, executable, "-test.run=^TestSelectionWriterProcess$")
			cmd.Dir, cmd.Env = c.Dir, append(c.Env, "GIT_TASK_SELECTION_WRITER=1")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("writer: %v %s", err, output)
			}
			planAfterWriter = selectionBytes(t, c)
		},
	}
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(ctx, []string{"complete", "--id", "task-001"}, "test", Streams{
		Out: &out, Err: &diagnostic,
		OpenDialogue: func() (Dialogue, error) { return dialogue, nil },
	}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 130 || !dialogue.closed || out.Len() != 0 {
		t.Fatalf("code=%d closed=%v out=%q diagnostic=%q", code, dialogue.closed, &out, &diagnostic)
	}
	remoteAfter := network("ls-remote", "origin", "refs/heads/git-task-plan")
	if !bytes.Equal(remoteBefore, remoteAfter) || !bytes.Equal(planAfterWriter, selectionBytes(t, c)) {
		t.Fatalf("cancelled command published another process action or rewrote its plan: remote before=%s after=%s", remoteBefore, remoteAfter)
	}
}
