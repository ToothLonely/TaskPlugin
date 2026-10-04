package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInformationCommands(t *testing.T) {
	t.Setenv("PATH", "")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"empty", nil, "Доступные команды:"},
		{"help", []string{"help"}, "Доступные команды:"},
		{"help alias", []string{"--help"}, "Доступные команды:"},
		{"version", []string{"version"}, "git-task test-version\n"},
		{"version alias", []string{"--version"}, "git-task test-version\n"},
		{"topic", []string{"help", "version"}, "Использование: git task version"},
		{"command help", []string{"version", "--help"}, "Использование: git task version"},
		{"flag before argument", []string{"help", "--help", "version"}, "Использование: git task help"},
		{"flag after argument", []string{"help", "version", "--help"}, "Использование: git task help"},
		{"separator", []string{"help", "--", "version"}, "Использование: git task version"},
		{"root separator", []string{"--", "version"}, "git-task test-version\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := Run(context.Background(), tc.args, "test-version", Streams{Out: &out, Err: &diagnostic})
			if code != 0 || !strings.Contains(out.String(), tc.want) || diagnostic.Len() != 0 {
				t.Fatalf("Run(%q) = %d, stdout=%q, stderr=%q", tc.args, code, &out, &diagnostic)
			}
		})
	}
}

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"unknown"}, {"-x"}, {"help", "unknown"}, {"help", "help", "version"},
		{"version", "extra"}, {"version", "--unknown"}, {"help", "--help", "--help"},
		{"help", "version", "--unknown"}, {"help", "--", "--help"},
		{"--help", "--version"}, {"version", "--help", "extra"}, {"version", "--help=false"},
		{"--help", "--help"}, {"--version", "--help"}, {"--version", "--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := Run(context.Background(), args, "test", Streams{Out: &out, Err: &diagnostic})
			if code != 2 || out.Len() != 0 || diagnostic.Len() == 0 {
				t.Fatalf("code=%d, stdout=%q, stderr=%q", code, &out, &diagnostic)
			}
		})
	}
}

func TestDoctorRequiresConnectedOperations(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"doctor"}, "test", Streams{Out: &out, Err: &diagnostic})
	if code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "не подключены") {
		t.Fatalf("doctor: code=%d, stdout=%q, stderr=%q", code, &out, &diagnostic)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, diagnostic bytes.Buffer
	if code := Run(ctx, []string{"version"}, "test", Streams{Out: &out, Err: &diagnostic}); code != 130 || out.Len() != 0 {
		t.Fatalf("code=%d, stdout=%q, stderr=%q", code, &out, &diagnostic)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestOutputFailure(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}} {
		var diagnostic bytes.Buffer
		if code := Run(context.Background(), args, "test", Streams{Out: failedWriter{}, Err: &diagnostic}); code != 1 {
			t.Fatalf("Run(%q) code=%d", args, code)
		}
	}
}
