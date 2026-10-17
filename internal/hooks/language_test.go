package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyDiagnosticWrapperCompatibility(t *testing.T) {
	i, dir := installFixture(t)
	ctx := context.Background()
	if _, err := i.Install(ctx); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/legacy-post-commit.sh")
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(strings.ReplaceAll(string(fixture), "@BINARY@", quote(filepath.ToSlash(i.Binary))))
	r := record{Version: 1, Event: "post-commit", Binary: i.Binary, Wrapper: legacy}
	journal, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "post-commit")
	if err := os.WriteFile(path, legacy, r.installed().mode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git-task-post-commit.json"), journal, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Diagnose(ctx); err != nil {
		t.Fatalf("legacy hook rejected: %v", err)
	}
	if changed, err := i.Install(ctx); err != nil || changed {
		t.Fatalf("legacy install should be a no-op: %v %v", changed, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, legacy) {
		t.Fatal("legacy wrapper changed")
	}
	if changed, err := i.Uninstall(ctx); err != nil || !changed {
		t.Fatalf("legacy uninstall: %v %v", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy wrapper remains: %v", err)
	}
}
