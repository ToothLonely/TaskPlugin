package diagnostic

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRenderPreservesCausesAndUserText(t *testing.T) {
	missing := New("задача не найдена")
	pathError := &os.PathError{Op: "open", Path: "данные.json", Err: os.ErrNotExist}
	err := Errorf("задача %s: %w", "задача не найдена", errors.Join(missing, pathError))
	var path *os.PathError
	if !errors.Is(err, missing) || !errors.Is(err, os.ErrNotExist) || !errors.As(err, &path) || path != pathError {
		t.Fatal("error identity or type lost")
	}
	for _, tc := range []struct {
		language Language
		prefix   string
	}{
		{English, "task задача не найдена: task not found\n"},
		{Russian, "задача задача не найдена: задача не найдена\n"},
		{Language("fr"), "task задача не найдена: task not found\n"},
	} {
		got := Render(tc.language, err)
		if got != tc.prefix+pathError.Error() {
			t.Errorf("Render(%s) = %q", tc.language, got)
		}
	}
	if err.Error() != "задача задача не найдена: задача не найдена\n"+pathError.Error() {
		t.Fatal("rendering mutated the original error")
	}
}

func TestRenderFormattingAndWrapping(t *testing.T) {
	a, b := New("неверный commit"), New("неверное дерево")
	wrapped := Errorf("%w: %w", a, b)
	if !errors.Is(wrapped, a) || !errors.Is(wrapped, b) || Render(English, wrapped) != "invalid commit: invalid tree" {
		t.Fatal("multiple wrapped causes lost")
	}
	err := Errorf("Git %q завершился с кодом %d", []string{"show", "ветка%w"}, 128)
	if got := Render(English, err); got != `Git ["show" "ветка%w"] exited with code 128` {
		t.Fatalf("formatting changed: %q", got)
	}
	foreign := fmt.Errorf("external: %w", errors.New("original output"))
	if Render(English, foreign) != foreign.Error() || Render(English, nil) != "" {
		t.Fatal("external error or nil changed")
	}
	multiple := fmt.Errorf("external: %w and %w", errors.New("first"), errors.New("second"))
	if Render(English, multiple) != multiple.Error() {
		t.Fatal("external multi-error context lost")
	}
	marshaler := &json.MarshalerError{Type: reflect.TypeOf(struct{}{}), Err: New("некорректные данные плана")}
	if !strings.HasSuffix(Render(English, marshaler), ": invalid plan data") {
		t.Fatal("JSON wrapper hid the diagnostic")
	}
	if strings.Contains(Render(English, err), "%!") {
		t.Fatal("invalid format")
	}
}
