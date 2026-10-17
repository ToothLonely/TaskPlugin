package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-task/internal/diagnostic"
)

func TestGitDiagnosticFallback(t *testing.T) {
	client := isolatedClient(t)
	client.Env = append(client.Env, "LC_ALL=fr_FR.UTF-8", "LANGUAGE=fr")
	_, err := client.Run(context.Background(), "not-a-real-git-command")
	var command *CommandError
	if !errors.As(err, &command) || command.Result.ExitCode == 0 {
		t.Fatalf("expected Git failure, got %v", err)
	}
	text := diagnostic.Render(diagnostic.English, err)
	if !strings.Contains(text, "exited with code") || !strings.Contains(text, "not a git command") {
		t.Fatalf("fallback diagnostic = %q", text)
	}
}
