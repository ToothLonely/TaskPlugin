package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEditorSelectionOrderAndMissingConfiguration(t *testing.T) {
	c := isolatedClient(t)
	ctx := context.Background()
	withEnv := func(values ...string) Client {
		t.Helper()
		copy := *c
		copy.Env = append(append([]string(nil), c.Env...), values...)
		return copy
	}
	for _, tc := range []struct {
		values []string
		want   string
	}{
		{[]string{"GIT_EDITOR=git-editor", "VISUAL=visual", "EDITOR=editor"}, "git-editor"},
		{[]string{"GIT_EDITOR=", "VISUAL=visual", "EDITOR=editor"}, "visual"},
		{[]string{"GIT_EDITOR=", "VISUAL=", "EDITOR=editor"}, "editor"},
	} {
		client := withEnv(tc.values...)
		if got, err := client.Editor(ctx); err != nil || got != tc.want {
			t.Fatalf("%v: %q %v", tc.values, got, err)
		}
	}
	if _, err := c.Run(ctx, "config", "--local", "core.editor", `"editor with spaces" --wait`); err != nil {
		t.Fatal(err)
	}
	client := withEnv("GIT_EDITOR=", "VISUAL=visual", "EDITOR=editor")
	if got, err := client.Editor(ctx); err != nil || got != `"editor with spaces" --wait` {
		t.Fatalf("core.editor precedence: %q %v", got, err)
	}
	client = withEnv("GIT_EDITOR=git-editor")
	if got, err := client.Editor(ctx); err != nil || got != "git-editor" {
		t.Fatalf("GIT_EDITOR precedence: %q %v", got, err)
	}
	if _, err := c.Run(ctx, "config", "--unset", "core.editor"); err != nil {
		t.Fatal(err)
	}
	client = withEnv("GIT_EDITOR=", "VISUAL=", "EDITOR=")
	if _, err := client.Editor(ctx); err == nil {
		t.Fatal("missing editor accepted")
	}
	if err := os.WriteFile(filepath.Join(c.Dir, ".git", "config"), []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Editor(ctx); err == nil {
		t.Fatal("invalid Git configuration hidden as absent editor")
	}
}
