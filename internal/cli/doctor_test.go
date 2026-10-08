package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
)

func TestDoctorSyntaxAndHelpBeforeDiscovery(t *testing.T) {
	for _, args := range [][]string{{"doctor", "--repair"}, {"doctor", "--repair=all"}, {"doctor", "--yes"}, {"doctor", "--yes=false"}, {"doctor", "extra"}, {"doctor", "--repair=unlock", "--repair=unlock"}, {"doctor", "--help", "--help"}, {"doctor", "--", "extra"}} {
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) { t.Fatal("opened on invalid syntax"); return nil, nil })
		if code != 2 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, &out, &diagnostic)
		}
	}
	for _, args := range [][]string{{"help", "doctor"}, {"doctor", "--help"}} {
		var out, diagnostic bytes.Buffer
		code := RunWithPlans(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) { t.Fatal("opened for help"); return nil, nil })
		if code != 0 || !strings.Contains(out.String(), "restore-backup") {
			t.Fatalf("help: %d %s %s", code, &out, &diagnostic)
		}
	}
}

func TestDoctorNoJournalNeedsNoConfirmation(t *testing.T) {
	p, _ := selectionFixture(t)
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"doctor", "--repair=recover-start"}, "test", Streams{Out: &out, Err: &diagnostic, OpenDialogue: func() (Dialogue, error) { t.Fatal("no-op prompted"); return nil, nil }}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 0 || !strings.Contains(out.String(), "Нет незавершённой операции") {
		t.Fatalf("no-op: %d %s %s", code, &out, &diagnostic)
	}
}

func TestDoctorRestoreConfirmationCancelAndRace(t *testing.T) {
	for _, mode := range []string{"no-terminal", "eof", "decline", "yes", "race"} {
		t.Run(mode, func(t *testing.T) {
			p, c := selectionFixture(t)
			path := filepath.Join(c.Dir, ".git-task", "plan.json")
			original := []byte(`{"format":"git-task","schema_version":3,`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			streams := Streams{Out: &out, Err: &diagnostic}
			args := []string{"doctor", "--repair=restore-backup"}
			want := 130
			dialogue := &scriptedDialogue{reader: bufio.NewReader(strings.NewReader(""))}
			switch mode {
			case "no-terminal":
				want = 1
			case "decline":
				dialogue.reader = bufio.NewReader(strings.NewReader("нет\n"))
			case "yes":
				args = append(args, "--yes")
				want = 0
			case "race":
				want = 1
				dialogue.reader = bufio.NewReader(strings.NewReader("да\n"))
				dialogue.before = func() {
					original = append(original, '\n')
					if err := os.WriteFile(path, original, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode != "no-terminal" && mode != "yes" {
				streams.OpenDialogue = func() (Dialogue, error) { return dialogue, nil }
			}
			code := RunWithPlans(context.Background(), args, "test", streams, func(context.Context) (*app.Plans, error) { return p, nil })
			if code != want {
				t.Fatalf("code=%d want=%d stdout=%s stderr=%s", code, want, &out, &diagnostic)
			}
			if mode != "yes" {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatal("unapproved or racing repair changed plan")
				}
				if mode != "no-terminal" && !dialogue.closed {
					t.Fatal("dialogue not closed")
				}
			}
		})
	}
}

func TestDoctorProblemExitAndReadOnlyOutput(t *testing.T) {
	p, c := selectionFixture(t)
	path := filepath.Join(c.Dir, ".git-task", "plan.json")
	data := []byte(`{"format":"git-task","schema_version":99}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"doctor"}, "test", Streams{Out: &out, Err: &diagnostic}, func(context.Context) (*app.Plans, error) { return p, nil })
	if code != 1 || !strings.Contains(out.String(), "99") || !strings.Contains(out.String(), "Следующее действие") {
		t.Fatalf("doctor: %d %s %s", code, &out, &diagnostic)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("unknown schema changed")
	}
}
