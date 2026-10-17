package diagnostic

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Language string

const (
	English Language = "en"
	Russian Language = "ru"
)

type message struct {
	format string
	args   []any
	cause  error
}

func New(text string) error {
	return &message{format: text}
}

func Errorf(format string, args ...any) error {
	return &message{format: format, args: append([]any(nil), args...), cause: fmt.Errorf(format, args...)}
}

func (m *message) Error() string {
	if m.cause != nil {
		return m.cause.Error()
	}
	return m.format
}

func (m *message) Unwrap() error { return m.cause }

func Render(language Language, err error) string {
	if err == nil {
		return ""
	}
	if m, ok := err.(*message); ok {
		format := m.format
		if language != Russian {
			if translated, ok := english[format]; ok {
				format = translated
			}
		}
		if m.cause == nil {
			return format
		}
		args := append([]any(nil), m.args...)
		for i, arg := range args {
			if nested, ok := arg.(error); ok {
				args[i] = renderedError(Render(language, nested))
			}
		}
		return fmt.Errorf(format, args...).Error()
	}
	if localized, ok := err.(interface{ Diagnostic() error }); ok {
		return Render(language, localized.Diagnostic())
	}
	if marshaler, ok := err.(*json.MarshalerError); ok {
		copy := *marshaler
		copy.Err = renderedError(Render(language, marshaler.Err))
		return copy.Error()
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var original, parts []string
		for _, nested := range joined.Unwrap() {
			if nested == nil {
				continue
			}
			original = append(original, nested.Error())
			parts = append(parts, Render(language, nested))
		}
		if strings.Join(original, "\n") == err.Error() {
			return strings.Join(parts, "\n")
		}
	}
	return err.Error()
}

type renderedError string

func (e renderedError) Error() string { return string(e) }
