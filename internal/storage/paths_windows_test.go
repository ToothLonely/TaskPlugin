package storage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsJunctionRejected(t *testing.T) {
	s, _ := fixture(t)
	destination := filepath.Join(filepath.Dir(s.dir), "junction-target")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	// mklink is a cmd builtin; both operands are test-owned generated paths.
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", s.dir, destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("junction: %v %s", err, output)
	}
	if _, err := s.Init(context.Background(), "main"); err == nil {
		t.Fatal("junction accepted")
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 0 {
		t.Fatalf("junction target changed: %v %v", entries, err)
	}
}

func TestWindowsReadOnlyPlanKeepsBackup(t *testing.T) {
	s, _ := initialized(t)
	snap := snapshot(t, s)
	path := filepath.Join(s.dir, "plan.json")
	original := read(t, path)
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	if _, err := s.Save(context.Background(), snap, added(t, snap, "new")); err == nil {
		t.Fatal("read-only file overwritten")
	}
	if string(read(t, path)) != string(original) || string(read(t, filepath.Join(s.dir, "plan.backup.json"))) != string(original) {
		t.Fatal("original or backup lost")
	}
}
