package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditorHelperProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--editor-helper" {
			continue
		}
		mode, path := os.Args[i+1], os.Args[i+2]
		if mode == "cancel" {
			os.Exit(7)
		}
		if err := os.WriteFile(path, []byte(`{"title":"Процесс редактора 🙂","description":""}`), 0600); err != nil {
			os.Exit(9)
		}
		os.Stdout.WriteString("editor output\n")
		os.Exit(0)
	}
}

func TestLaunchEditorUsesSeparateArgumentsAndDiagnosticStream(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "документ с пробелами & (edit).json")
	var out, diagnostic bytes.Buffer
	streams := Streams{Out: &out, Err: &diagnostic}
	quotedBinary := `"` + strings.ReplaceAll(binary, `"`, `\"`) + `"`
	command := quotedBinary + " -test.run=^TestEditorHelperProcess$ -- --editor-helper success"
	if err := launchEditor(context.Background(), command, path, filepath.Dir(path), streams); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("Процесс редактора")) || out.Len() != 0 || !bytes.Contains(diagnostic.Bytes(), []byte("editor output")) {
		t.Fatalf("process editor: %v %q %q %q", err, data, &out, &diagnostic)
	}
	before := append([]byte(nil), data...)
	command = quotedBinary + " -test.run=^TestEditorHelperProcess$ -- --editor-helper cancel"
	if err := launchEditor(context.Background(), command, path, filepath.Dir(path), streams); !errors.Is(err, context.Canceled) {
		t.Fatalf("nonzero editor should cancel: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(data, before) {
		t.Fatalf("cancel changed document: %v", err)
	}
	if err := launchEditor(context.Background(), `"`+filepath.Join(t.TempDir(), "absent-editor")+`"`, path, filepath.Dir(path), streams); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("launch failure should be ordinary error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := launchEditor(ctx, command, path, filepath.Dir(path), streams); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation: %v", err)
	}
}
