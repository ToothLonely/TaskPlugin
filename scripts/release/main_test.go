package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestArchiveContentsAndModes(t *testing.T) {
	entries := []entry{{"git-task", []byte("binary"), 0755}, {"README.md", []byte("инструкция"), 0644}}
	for _, windows := range []bool{false, true} {
		name := "tar"
		if windows {
			name = "zip"
		}
		t.Run(name, func(t *testing.T) {
			data, err := pack(entries, windows)
			if err != nil {
				t.Fatal(err)
			}
			second, err := pack(entries, windows)
			if err != nil || !bytes.Equal(data, second) {
				t.Fatalf("unstable archive: %v", err)
			}
			if windows {
				reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				if len(reader.File) != len(entries) {
					t.Fatalf("files: %d", len(reader.File))
				}
				for i, file := range reader.File {
					stream, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					contents, readErr := io.ReadAll(stream)
					closeErr := stream.Close()
					if readErr != nil || closeErr != nil || file.Name != entries[i].name || int64(file.Mode().Perm()) != entries[i].mode || !bytes.Equal(contents, entries[i].data) {
						t.Fatalf("zip entry %s: read=%v close=%v mode=%o", file.Name, readErr, closeErr, file.Mode())
					}
				}
				return
			}
			compressed, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer compressed.Close()
			reader := tar.NewReader(compressed)
			for _, item := range entries {
				header, err := reader.Next()
				if err != nil {
					t.Fatal(err)
				}
				contents, err := io.ReadAll(reader)
				if err != nil || header.Name != item.name || header.Mode != item.mode || !bytes.Equal(contents, item.data) {
					t.Fatalf("tar entry %s: err=%v mode=%o", header.Name, err, header.Mode)
				}
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatalf("unexpected extra entry: %v", err)
			}
		})
	}
}

func TestReleaseInputsExcludeLocalAgentFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cmd", "internal", "scripts/release"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{
		"go.mod":                              "module example\n",
		"README.md":                           "usage\n",
		"cmd/main.go":                         "package main\n",
		"scripts/installer/build-windows.ps1": "native installer template\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(name))), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	assets, sourceHash, err := inputs(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PLAN.md", "AGENTS.md", "SKILLS.md", "docs/RELEASE.md", "docs/INSTALL.md", "docs/DECISIONS.md", "docs/notes/new.md", "cmd/NOTES.md", "scripts/release/REVIEW.md"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("local instructions"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	withLocalFiles, withLocalHash, err := inputs(root)
	if err != nil {
		t.Fatal(err)
	}
	if sourceHash != withLocalHash || len(assets) != len(withLocalFiles) {
		t.Fatal("local documents changed release inputs")
	}
	found := make(map[string]bool)
	for i, asset := range withLocalFiles {
		if asset.name != assets[i].name || !bytes.Equal(asset.data, assets[i].data) {
			t.Fatalf("release asset changed: %s", asset.name)
		}
		found[asset.name] = true
		if asset.name == "INPUT-SHA256SUMS" {
			for _, name := range []string{"PLAN.md", "AGENTS.md", "SKILLS.md", "LICENSE-STATUS.md", "docs/", "cmd/NOTES.md", "scripts/release/REVIEW.md"} {
				if strings.Contains(string(asset.data), name) {
					t.Fatalf("local document included in input hashes: %s", name)
				}
			}
		}
	}
	for _, name := range []string{"README.md", "INPUT-SHA256SUMS", "GO-LICENSE", "GO-PATENTS"} {
		if !found[name] {
			t.Fatalf("release asset missing: %s", name)
		}
	}
}

func TestReleaseRejectsInvalidRequest(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-version", "dev", "-out", ".tools/unused"},
		{"-version", "1.0.0;unsafe", "-out", ".tools/unused"},
		{"-version", "1.0.0", "-out", "../outside"},
		{"-version", "1.0.0", "-out", "."},
		{"-version", "1.0.0", "-out", ".tools/unused", "extra"},
		{"-version", "1.0.0", "-out", ".tools/unused", "-mode", "unknown"},
		{"-version", "1.0.0", "-out", ".tools/unused", "-mode", "bundle"},
	} {
		if err := run(context.Background(), args); err == nil {
			t.Fatalf("accepted invalid request %q", args)
		}
	}
}

func TestReleaseEnvironment(t *testing.T) {
	t.Setenv("GOOS", "foreign")
	t.Setenv("GOARCH", "arm64")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOFLAGS", "-race")
	t.Setenv("GOWORK", "foreign")
	values := map[string]string{}
	for _, value := range buildEnvironment("linux") {
		key, content, _ := strings.Cut(value, "=")
		if _, exists := values[strings.ToUpper(key)]; exists {
			t.Fatalf("duplicate variable: %s", key)
		}
		values[strings.ToUpper(key)] = content
	}
	for key, want := range map[string]string{"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0", "GOFLAGS": "", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOENV": "off", "GOPROXY": "off"} {
		if values[key] != want {
			t.Fatalf("%s=%q, want %q", key, values[key], want)
		}
	}
}

func TestReleaseOutputDirectory(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "releases", "candidate")
	t.Chdir(root)
	for _, out := range []string{want, filepath.Join("releases", "candidate")} {
		got, err := outputDirectory(context.Background(), root, out)
		if err != nil || got != want {
			t.Fatalf("output %q: got %q, err %v, want %q", out, got, err, want)
		}
	}
	for _, out := range []string{root, filepath.Join(root, "..", "outside"), filepath.Join(root, "..", filepath.Base(root)+"-other", "candidate")} {
		if got, err := outputDirectory(context.Background(), root, out); err == nil {
			t.Fatalf("accepted output outside project: %q => %q", out, got)
		}
	}
	file := filepath.Join(root, "ordinary-file")
	if err := os.WriteFile(file, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{file, filepath.Join(file, "candidate")} {
		if got, err := outputDirectory(context.Background(), root, out); err == nil {
			t.Fatalf("accepted non-directory output: %q => %q", out, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := outputDirectory(ctx, root, want); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled path validation: %v", err)
	}
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(root, "linked")
		if err := os.Symlink(t.TempDir(), link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if got, err := outputDirectory(context.Background(), root, filepath.Join(link, "candidate")); err == nil {
			t.Fatalf("accepted symlink output: %q", got)
		}
	})
}

func TestReleaseOutputPathCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path case semantics")
	}
	if root := os.Getenv("GIT_TASK_RELEASE_CASE_ROOT"); root != "" {
		out := os.Getenv("GIT_TASK_RELEASE_CASE_OUT")
		got, err := outputDirectory(context.Background(), root, out)
		want := filepath.Join(root, "releases", "candidate")
		if err != nil || got != want {
			t.Fatalf("case output %q: got %q, err %v, want %q", out, got, err, want)
		}
		return
	}
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	volume := filepath.VolumeName(root)
	variedVolume := strings.ToLower(volume)
	if variedVolume == volume {
		variedVolume = strings.ToUpper(volume)
	}
	for _, tc := range []struct{ name, root string }{
		{"drive", variedVolume + root[len(volume):]},
		{"directories", volume + strings.ToUpper(root[len(volume):])},
		{"drive_and_directories", variedVolume + strings.ToUpper(root[len(volume):])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestReleaseOutputPathCase$")
			command.Env = append(os.Environ(), "GIT_TASK_RELEASE_CASE_ROOT="+root, "GIT_TASK_RELEASE_CASE_OUT="+filepath.Join(tc.root, "releases", "candidate"))
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("path validation did not finish: %v\n%s", ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("case path validation: %v\n%s", err, output)
			}
		})
	}
}
