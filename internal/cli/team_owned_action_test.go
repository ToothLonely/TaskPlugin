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

func TestCancelledSelectAndEditorDoNotPublishConcurrentWriter(t *testing.T) {
	for _, mode := range []string{"select", "editor-cancel", "editor-conflict"} {
		t.Run(mode, func(t *testing.T) {
			p, c := selectionFixture(t)
			ctx := context.Background()
			remote := filepath.Join(t.TempDir(), "remote.git")
			testrepo.Run(t, c, "init", "--bare", "--initial-branch=main", remote)
			testrepo.Run(t, c, "remote", "add", "origin", remote)
			testrepo.Run(t, c, "config", "--local", "core.editor", "isolated-editor")
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
			var afterWriter []byte
			writeOtherProcess := func() {
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
				afterWriter = selectionBytes(t, c)
			}
			var out, diagnostic bytes.Buffer
			streams := Streams{Out: &out, Err: &diagnostic}
			args := []string{"edit", "--id", "task-001"}
			expectedCode := 130
			if mode == "select" {
				args = []string{"start", "cancelled", "--select"}
				dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader("0\n")), before: writeOtherProcess}
				streams.OpenDialogue = func() (Dialogue, error) { return dialogue, nil }
			} else {
				streams.RunEditor = func(_ context.Context, _, path, _ string) error {
					writeOtherProcess()
					if mode == "editor-cancel" {
						return context.Canceled
					}
					return os.WriteFile(path, []byte(`{"title":"Cannot save stale preview","description":""}`), 0600)
				}
				if mode == "editor-conflict" {
					expectedCode = 1
				}
			}
			code := RunWithPlans(ctx, args, "test", streams, func(context.Context) (*app.Plans, error) { return p, nil })
			if code != expectedCode || out.Len() != 0 || len(afterWriter) == 0 {
				t.Fatalf("result: code=%d out=%q diagnostic=%q", code, &out, &diagnostic)
			}
			remoteAfter := network("ls-remote", "origin", "refs/heads/git-task-plan")
			if !bytes.Equal(remoteBefore, remoteAfter) || !bytes.Equal(afterWriter, selectionBytes(t, c)) {
				t.Fatalf("rejected %s published another process action", mode)
			}
		})
	}
}
