package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"git-task/internal/testrepo"
)

func TestArchiveInstaller(t *testing.T) {
	version := "0.0.0-installtest"
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binaryName := "git-task"
	extension := ".tar.gz"
	scriptName := "install.sh"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
		extension = ".zip"
		scriptName = "install.ps1"
	}
	binaryPath := filepath.Join(t.TempDir(), binaryName)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-ldflags=-X=main.version="+version, "-o", binaryPath, "./cmd/git-task")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build installer binary: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	archiveName := "git-task_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + extension
	archive, err := pack([]entry{{binaryName, binary, 0755}}, runtime.GOOS == "windows")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "checksum", "foreign-alias", "occupied-directory", "tracked-directory"} {
		t.Run(scenario, func(t *testing.T) {
			c := testrepo.New(t)
			c.Dir = filepath.Join(c.Dir, "project's directory")
			if err := os.Mkdir(c.Dir, 0755); err != nil {
				t.Fatal(err)
			}
			testrepo.Run(t, c, "init", "--initial-branch=main", "--template=")
			downloads := t.TempDir()
			if err := os.WriteFile(filepath.Join(downloads, archiveName), archive, 0644); err != nil {
				t.Fatal(err)
			}
			sum := digest(archive)
			if scenario == "checksum" {
				sum = strings.Repeat("0", 64)
			}
			if err := os.WriteFile(filepath.Join(downloads, "SHA256SUMS"), []byte(sum+"  "+archiveName+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(c.Dir, ".tools", "git-task", version, runtime.GOOS+"-"+runtime.GOARCH)
			sentinel := filepath.Join(destination, "keep")
			if scenario == "tracked-directory" {
				sentinel = filepath.Join(c.Dir, ".tools", "git-task", "keep")
			}
			if scenario == "foreign-alias" {
				testrepo.Run(t, c, "config", "--local", "alias.task", "status")
			}
			if scenario == "occupied-directory" || scenario == "tracked-directory" {
				if err := os.MkdirAll(filepath.Dir(sentinel), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(sentinel, []byte("preserve"), 0644); err != nil {
					t.Fatal(err)
				}
				if scenario == "tracked-directory" {
					testrepo.Run(t, c, "add", ".tools/git-task")
				}
			}
			configPath := filepath.Join(c.Dir, ".git", "config")
			configBefore, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var install *exec.Cmd
			scriptPath := filepath.Join(root, "scripts", scriptName)
			if runtime.GOOS == "windows" {
				install = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-File", scriptPath, "-Version", version, "-FromDirectory", downloads)
			} else {
				install = exec.CommandContext(ctx, "sh", scriptPath, "--version", version, "--from-directory", downloads)
			}
			install.Dir, install.Env = c.Dir, c.Env
			output, err := install.CombinedOutput()
			if scenario == "success" {
				if err != nil {
					t.Fatalf("install: %v\n%s", err, output)
				}
				if got := string(bytes.TrimSpace(testrepo.Run(t, c, "task", "version"))); got != "git-task "+version {
					t.Fatalf("installed alias version: %q", got)
				}
				if files := testrepo.Run(t, c, "ls-files", "--others", "--exclude-standard", "--", ".tools"); len(files) != 0 {
					t.Fatalf("installation not excluded: %s", files)
				}
				return
			}
			if err == nil {
				t.Fatalf("invalid installation accepted: %s", scenario)
			}
			expected := map[string]string{
				"checksum":           "Archive checksum mismatch",
				"foreign-alias":      "Existing task alias belongs to another installation",
				"occupied-directory": "This version already exists",
				"tracked-directory":  "Installation directory contains tracked files",
			}[scenario]
			if !bytes.Contains(output, []byte(expected)) {
				t.Fatalf("unexpected installer failure: %v\n%s", err, output)
			}
			configAfter, err := os.ReadFile(configPath)
			if err != nil || !bytes.Equal(configBefore, configAfter) {
				t.Fatalf("rejected installation changed Git config: %v", err)
			}
			if scenario == "occupied-directory" || scenario == "tracked-directory" {
				data, err := os.ReadFile(sentinel)
				if err != nil || string(data) != "preserve" {
					t.Fatalf("existing file changed: %v", err)
				}
			} else if _, err := os.Stat(filepath.Join(destination, binaryName)); !os.IsNotExist(err) {
				t.Fatalf("rejected installation wrote binary: %v", err)
			}
		})
	}
}

func TestInstallerVersionRendering(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.ps1", "install.sh"} {
		if err := os.WriteFile(filepath.Join(root, "scripts", name), []byte("version=__GIT_TASK_VERSION__"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := installers(root, "0.1.0")
	if err != nil || len(assets) != 2 {
		t.Fatalf("render installers: %v", err)
	}
	for _, asset := range assets {
		if string(asset.data) != "version=0.1.0" {
			t.Fatalf("unrendered installer: %s", asset.data)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "install.sh"), []byte("no version"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := installers(root, "0.1.0"); err == nil {
		t.Fatal("installer without version placeholder accepted")
	}
}

func TestReleaseARMEnvironment(t *testing.T) {
	t.Setenv("GOOS", "windows")
	t.Setenv("GOARCH", "amd64")
	values := make(map[string]string)
	for _, value := range buildEnvironment("darwin/arm64") {
		key, data, _ := strings.Cut(value, "=")
		if _, exists := values[strings.ToUpper(key)]; exists {
			t.Fatalf("duplicate environment variable: %s", key)
		}
		values[strings.ToUpper(key)] = data
	}
	if values["GOOS"] != "darwin" || values["GOARCH"] != "arm64" || values["CGO_ENABLED"] != "0" {
		t.Fatalf("incorrect native target: %v", values)
	}
}
