package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseOutputJunctionRejected(t *testing.T) {
	for _, placement := range []string{"inside_root", "outside_root"} {
		t.Run(placement, func(t *testing.T) {
			work := t.TempDir()
			root := filepath.Join(work, "project")
			if err := os.MkdirAll(filepath.Join(root, "releases"), 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(work, "target")
			if placement == "inside_root" {
				target = filepath.Join(root, "target")
			}
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(target, "preserve.txt")
			original := []byte("unchanged junction destination")
			if err := os.WriteFile(marker, original, 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "releases", "junction")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "cmd.exe", "/d", "/c", "mklink", "/J", link, target)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("create junction: %v\n%s", err, output)
			}
			t.Cleanup(func() {
				if err := os.Remove(link); err != nil {
					t.Errorf("remove junction: %v", err)
				}
			})
			for _, out := range []string{link, filepath.Join(link, "candidate"), filepath.Join(link, "new", "candidate")} {
				if got, err := outputDirectory(ctx, root, out); err == nil {
					t.Fatalf("accepted junction output %q => %q", out, got)
				}
			}
			t.Chdir(root)
			if err := run(ctx, []string{"-version", "0.1.0-rc.1", "-out", filepath.Join(link, "candidate")}); err == nil {
				t.Fatal("release accepted junction output")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 1 || entries[0].Name() != "preserve.txt" {
				t.Fatalf("junction destination changed: %v, %v", entries, err)
			}
			contents, err := os.ReadFile(marker)
			if err != nil || string(contents) != string(original) {
				t.Fatalf("junction destination marker changed: %q, %v", contents, err)
			}
		})
	}
}
