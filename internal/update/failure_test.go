package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func failureInstallation(t *testing.T, dir string, data []byte) Installation {
	t.Helper()
	path := filepath.Join(dir, "git-task.exe")
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	i := Installation{Path: path, Owner: "portable", Version: "1.0.0", Hash: hex.EncodeToString(sum[:]), Mode: 0755}
	raw, err := json.Marshal(portableReceipt{Owner: i.Owner, Version: i.Version, SHA256: i.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "git-task-portable.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return i
}

func TestUpdateForeignPackage(t *testing.T) {
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/usr/bin/env"); err != nil {
			t.Fatal(err)
		}
		if owner, err := nativeOwner(context.Background(), "/usr/bin/env", "1.0.0"); err == nil {
			t.Fatalf("foreign package accepted: %q", owner)
		}
		return
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	if _, err := Inspect(context.Background(), path, "1.0.0"); err == nil {
		t.Fatal("unowned executable accepted")
	}
}

func TestUpdateCrashBoundaries(t *testing.T) {
	if dir := os.Getenv("UPDATE_CRASH_DIR"); dir != "" {
		i := failureInstallation(t, dir, []byte("old"))
		sum := sha256.Sum256([]byte("new"))
		err := applyPortableSteps(context.Background(), i, []byte("new"), "2.0.0", hex.EncodeToString(sum[:]), func(context.Context, string, string) (Installation, error) { return i, nil }, func(context.Context, string, string) error { return nil }, func(stage string) {
			if stage == os.Getenv("UPDATE_CRASH_STAGE") {
				os.Exit(78)
			}
		})
		t.Fatalf("child did not crash: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"journal", "backup", "binary", "receipt-backup", "receipt"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestUpdateCrashBoundaries$")
			cmd.Env = append(os.Environ(), "UPDATE_CRASH_DIR="+dir, "UPDATE_CRASH_STAGE="+stage)
			if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 78 {
				t.Fatalf("crash: %v", err)
			}
			path := filepath.Join(dir, "git-task.exe")
			oldPath := path + ".update-backup"
			if stage == "journal" {
				oldPath = path
			}
			old, err := os.ReadFile(oldPath)
			if err != nil || string(old) != "old" {
				t.Fatalf("old copy lost: %q %v", old, err)
			}
			if stage == "receipt-backup" || stage == "receipt" {
				if err := recoverPortable(path, "2.0.0"); err != nil {
					t.Fatal(err)
				}
				if err := recoverPortable(path, "2.0.0"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := recoverPortable(path, "1.0.0"); err == nil {
					t.Fatal("ambiguous state accepted")
				}
				if data, err := os.ReadFile(oldPath); err != nil || string(data) != "old" {
					t.Fatal("recovery lost old copy")
				}
			}
		})
	}
}

func TestUpdateRunningExecutable(t *testing.T) {
	if os.Getenv("UPDATE_BUSY_CHILD") == "1" {
		os.Stdout.WriteString("ready\n")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	i := failureInstallation(t, t.TempDir(), data)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, i.Path, "-test.run=^TestUpdateRunningExecutable$")
	cmd.Env = append(os.Environ(), "UPDATE_BUSY_CHILD=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child: %q %v", line, err)
	}
	err = applyPortable(ctx, i, data, "2.0.0", i.Hash, func(context.Context, string, string) (Installation, error) { return i, nil }, func(context.Context, string, string) error { return nil })
	if err != nil {
		hash, _, readErr := fileHash(i.Path)
		if readErr != nil || hash != i.Hash {
			t.Fatalf("busy failure lost original: %v / %v", err, readErr)
		}
	}
	if _, err := in.Write([]byte("stop\n")); err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateForeignPaths(t *testing.T) {
	dir := t.TempDir()
	i := failureInstallation(t, dir, []byte("original"))
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, filepath.Base(i.Path)), []byte("foreign"), 0755); err != nil {
		t.Fatal(err)
	}
	name := "git-task"
	if filepath.Ext(i.Path) == ".exe" {
		name = "git-task.exe"
	}
	if name == "git-task.exe" {
		if err := os.WriteFile(filepath.Join(other, "git-task"), []byte("foreign"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", other)
	if err := uniqueExecutable(i.Path); err == nil {
		t.Fatal("multiple copies accepted")
	}
	if _, _, err := fileHash(dir); err == nil {
		t.Fatal("directory accepted as executable")
	}
	link := filepath.Join(dir, "linked.exe")
	if err := os.Symlink(i.Path, link); err != nil {
		t.Fatalf("symlink coverage unavailable: %v", err)
	}
	if _, _, err := fileHash(link); err == nil {
		t.Fatal("symlink accepted as executable")
	}
	if err := os.WriteFile(filepath.Join(dir, "git-task-portable.json"), []byte("foreign receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, err := Inspect(context.Background(), i.Path, i.Version); err == nil {
		t.Fatal("foreign receipt accepted")
	}
}
