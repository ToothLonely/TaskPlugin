package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"git-task/internal/app"
	"git-task/internal/testrepo"
)

func TestTerminalDetectionRejectsRedirectedStreams(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for _, pair := range [][2]*os.File{{file, file}, {read, write}, {null, null}} {
		if isTerminal(pair[0]) {
			t.Fatalf("%s detected as terminal", pair[0].Name())
		}
		if dialogue, err := OpenTerminalDialogue(pair[0], pair[1]); err == nil || dialogue != nil {
			t.Fatalf("redirected dialogue=%v err=%v", dialogue, err)
		}
	}
}

func TestPollingDialogueCancellationUnblocksRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses console events; polling file deadlines apply to Unix")
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()
	dialogue, err := openPollingDialogue(read)
	if err != nil {
		t.Fatal(err)
	}
	defer dialogue.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := dialogue.ReadLine(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestSelectOutputFailureBeforeMutation(t *testing.T) {
	p, c := selectionFixture(t)
	data, refs, head := selectionBytes(t, c), testrepo.Run(t, c, "show-ref"), testrepo.Run(t, c, "rev-parse", "HEAD")
	dialogue := &scriptedDialogue{reader: nil}
	_, err := chooseStart(context.Background(), p, app.StartOptions{Branch: "feature"}, Streams{Err: failedWriter{}, OpenDialogue: func() (Dialogue, error) { return dialogue, nil }})
	if err == nil || !dialogue.closed {
		t.Fatalf("output error=%v closed=%v", err, dialogue.closed)
	}
	selectionUnchanged(t, c, data, refs, head)
}
