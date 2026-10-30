package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeUpdateInDisposableVM(t *testing.T) {
	if os.Getenv("GIT_TASK_UPDATE_DISPOSABLE_VM") != "1" {
		t.Skip("requires an explicitly designated disposable VM with candidate N installed")
	}
	value := func(key string) string {
		t.Helper()
		v := os.Getenv(key)
		if v == "" {
			t.Fatalf("missing %s", key)
		}
		return v
	}
	path := value("GIT_TASK_UPDATE_EXECUTABLE")
	payload := value("GIT_TASK_UPDATE_INSTALLER")
	previous := value("GIT_TASK_UPDATE_PREVIOUS")
	next := value("GIT_TASK_UPDATE_NEXT")
	expected := value("GIT_TASK_UPDATE_BINARY_SHA256")
	if !filepath.IsAbs(path) || !filepath.IsAbs(payload) || !validHash(expected) {
		t.Fatal("absolute candidate paths and SHA-256 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	i, err := Inspect(ctx, path, previous)
	if err != nil {
		t.Fatal(err)
	}
	if i.Owner == "portable" {
		t.Fatal("native package installation required")
	}
	data, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unlock(); err != nil {
			t.Error(err)
		}
	}()
	if err := installNative(ctx, i, filepath.Base(payload), data, next, expected, os.Stdout, os.Stderr); err != nil {
		t.Fatal(err)
	}
	updated, err := Inspect(ctx, path, next)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Owner != i.Owner || updated.Hash != expected {
		t.Fatalf("registration/hash not preserved: %+v", updated)
	}
}
