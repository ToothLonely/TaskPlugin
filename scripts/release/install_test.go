package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"git-task/internal/testrepo"
)

func TestNativeInstaller(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	version := "0.0.0-installtest"
	assets, sourceHash, err := inputs(root)
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := nativePackages(ctx, root, output, version, sourceHash, assets); err != nil {
		t.Fatal(err)
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	for _, name := range nativeNames(version, platform) {
		t.Run(filepath.Ext(name), func(t *testing.T) {
			packagePath := filepath.Join(output, name)
			extracted := filepath.Join(t.TempDir(), "extracted")
			var binary string
			switch filepath.Ext(name) {
			case ".msi":
				command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-File", filepath.Join(root, "scripts", "installer", "inspect-windows.ps1"), "-Package", packagePath, "-Destination", extracted)
				if data, err := command.CombinedOutput(); err != nil {
					t.Fatalf("extract MSI: %v\n%s", err, data)
				}
				binary = filepath.Join(extracted, "git-task.exe")
			case ".deb":
				command := exec.CommandContext(ctx, "dpkg-deb", "--extract", packagePath, extracted)
				if data, err := command.CombinedOutput(); err != nil {
					t.Fatalf("extract DEB: %v\n%s", err, data)
				}
				binary = filepath.Join(extracted, "usr", "bin", "git-task")
			case ".rpm":
				if err := os.Mkdir(extracted, 0755); err != nil {
					t.Fatal(err)
				}
				data, err := exec.CommandContext(ctx, "rpm2cpio", packagePath).Output()
				if err != nil {
					t.Fatal(err)
				}
				command := exec.CommandContext(ctx, "cpio", "-id", "--quiet")
				command.Dir, command.Stdin = extracted, bytes.NewReader(data)
				if data, err := command.CombinedOutput(); err != nil {
					t.Fatalf("extract RPM: %v\n%s", err, data)
				}
				binary = filepath.Join(extracted, "usr", "bin", "git-task")
			case ".pkg":
				command := exec.CommandContext(ctx, "/usr/sbin/pkgutil", "--expand-full", packagePath, extracted)
				if data, err := command.CombinedOutput(); err != nil {
					t.Fatalf("extract PKG: %v\n%s", err, data)
				}
				if err := filepath.WalkDir(extracted, func(path string, item os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !item.IsDir() && strings.HasSuffix(filepath.ToSlash(path), "/usr/local/bin/git-task") {
						binary = path
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if binary == "" {
				t.Fatal("installer did not contain CLI")
			}
			c := testrepo.New(t)
			testrepo.Commit(t, c)
			c.Env = append(c.Env, "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
			if versionOut := string(bytes.TrimSpace(testrepo.Run(t, c, "task", "version"))); versionOut != "git-task "+version {
				t.Fatalf("wrong installed version: %s", versionOut)
			}
			testrepo.Run(t, c, "task", "init")
			testrepo.Run(t, c, "task", "add", "Installed task")
			testrepo.Run(t, c, "task", "status")
			if alias, err := c.Run(ctx, "config", "--local", "--get", "alias.task"); err == nil || len(alias.Stdout) != 0 {
				t.Fatal("native install required a project alias")
			}
		})
	}
}

func TestNativeInstallerReceipts(t *testing.T) {
	directory := t.TempDir()
	version, sourceHash := "0.1.0-rc.3", "source snapshot"
	for _, platform := range []string{"windows/amd64", "linux/amd64", "darwin/arm64"} {
		receipt := nativeReceipt{Version: version, Platform: platform, SourceSHA256: sourceHash}
		for _, name := range nativeNames(version, platform) {
			data := []byte(name)
			if err := os.WriteFile(filepath.Join(directory, name), data, 0644); err != nil {
				t.Fatal(err)
			}
			receipt.Files = append(receipt.Files, releaseFile{Name: name, SHA256: digest(data)})
		}
		data, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, strings.ReplaceAll(platform, "/", "-")+".json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := installerInputs(directory, version, sourceHash)
	if err != nil || len(assets) != 4 {
		t.Fatalf("load installers: %v", err)
	}
	if _, err := installerInputs(directory, version, "other source"); err == nil {
		t.Fatal("installers from another commit accepted")
	}
	name := nativeNames(version, "windows/amd64")[0]
	if err := os.WriteFile(filepath.Join(directory, name), []byte("corrupted"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := installerInputs(directory, version, sourceHash); err == nil {
		t.Fatal("corrupted installer accepted")
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
		t.Fatalf("incorrect release target: %v", values)
	}
}
