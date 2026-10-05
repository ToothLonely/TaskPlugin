package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func candidateBinary(t *testing.T) string {
	t.Helper()
	path := os.Getenv("GIT_TASK_ACCEPTANCE_BINARY")
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		t.Fatal("GIT_TASK_ACCEPTANCE_BINARY must be absolute")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if expected := os.Getenv("GIT_TASK_ACCEPTANCE_SHA256"); expected == "" || digest != expected {
		t.Fatalf("candidate SHA-256 %s differs from required %q", digest, expected)
	}
	t.Logf("candidate=%s sha256=%s", path, digest)
	return path
}
