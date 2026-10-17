package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-task/internal/app"
	"git-task/internal/diagnostic"
	"git-task/internal/storage"
)

func TestMain(m *testing.M) {
	if os.Setenv("LC_ALL", "ru_RU.UTF-8") != nil || os.Setenv("LANGUAGE", "") != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestSelectDiagnosticLanguage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    map[string]string
		system string
		want   diagnostic.Language
	}{
		{"system Russian", nil, "ru-RU", diagnostic.Russian},
		{"system English", nil, "en-US", diagnostic.English},
		{"unsupported system", nil, "de-DE", diagnostic.English},
		{"unknown system", nil, "", diagnostic.English},
		{"LC_ALL wins", map[string]string{"LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "ru_RU", "LANG": "ru"}, "ru", diagnostic.English},
		{"messages wins", map[string]string{"LC_MESSAGES": "ru_RU.UTF-8", "LANG": "en_US"}, "en", diagnostic.Russian},
		{"LANG", map[string]string{"LANG": "RU-ru"}, "en", diagnostic.Russian},
		{"unsupported locale", map[string]string{"LANG": "ja_JP.UTF-8"}, "ru", diagnostic.English},
		{"invalid locale", map[string]string{"LANG": "garbage"}, "ru", diagnostic.English},
		{"C ignores LANGUAGE", map[string]string{"LC_ALL": "C.UTF-8", "LANGUAGE": "ru"}, "ru", diagnostic.English},
		{"POSIX ignores LANGUAGE", map[string]string{"LANG": "POSIX", "LANGUAGE": "ru"}, "ru", diagnostic.English},
		{"LANGUAGE preference", map[string]string{"LANG": "en_US.UTF-8", "LANGUAGE": "ru:en"}, "en", diagnostic.Russian},
		{"unsupported preferred language", map[string]string{"LANG": "ru_RU", "LANGUAGE": "fr:ru"}, "ru", diagnostic.English},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := selectLanguage(func(key string) string { return tc.env[key] }, func() string { return tc.system })
			if got != tc.want {
				t.Fatalf("language = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocalizedErrors(t *testing.T) {
	for _, tc := range []struct {
		locale string
		want   string
	}{
		{"ru_RU.UTF-8", "Ошибка: неизвестная команда"},
		{"en_US.UTF-8", "Error: unknown command"},
		{"fr_FR.UTF-8", "Error: unknown command"},
		{"C", "Error: unknown command"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.locale)
			t.Setenv("LANGUAGE", "")
			var out, errOut bytes.Buffer
			code := RunWithPlans(context.Background(), []string{"моя-команда"}, "test", Streams{Out: &out, Err: &errOut}, func(context.Context) (*app.Plans, error) {
				t.Fatal("invalid command opened a repository")
				return nil, nil
			})
			if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), tc.want) || !strings.Contains(errOut.String(), `"моя-команда"`) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, &out, &errOut)
			}
		})
	}
}

func TestLocalizedNestedFailureAndCancellation(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANGUAGE", "")
	var out, errOut bytes.Buffer
	open := func(context.Context) (*app.Plans, error) {
		return nil, diagnostic.Errorf("импорт %s: %w", "мой-план.json", storage.ErrLocked)
	}
	code := RunWithPlans(context.Background(), []string{"status"}, "test", Streams{Out: &out, Err: &errOut}, open)
	if code != 1 || !strings.Contains(errOut.String(), "Error: import мой-план.json: storage locked:") {
		t.Fatalf("code=%d stderr=%q", code, &errOut)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errOut.Reset()
	code = Run(ctx, []string{"version"}, "test", Streams{Out: &out, Err: &errOut})
	if code != 130 || !strings.Contains(errOut.String(), "Error: operation canceled:") {
		t.Fatalf("code=%d stderr=%q", code, &errOut)
	}
}

func TestLocalizedHookFailure(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("GIT_TASK_OPERATION", "")
	t.Setenv("GIT_TASK_TRANSPORT", "")
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"_hook", "unknown-event"}, "test", Streams{Out: &out, Err: &errOut})
	if code != 0 || !strings.Contains(errOut.String(), "tracking not performed: unknown hook event") || !strings.Contains(errOut.String(), "already occurred") {
		t.Fatalf("code=%d stderr=%q", code, &errOut)
	}
}

func TestLocalizedDoctorReport(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "")
	plans, client := selectionFixture(t)
	path := filepath.Join(client.Dir, ".git-task", "plan.json")
	before := []byte(`{"format":"git-task","schema_version":99}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := RunWithPlans(context.Background(), []string{"doctor"}, "test", Streams{Out: &out, Err: &errOut}, func(context.Context) (*app.Plans, error) { return plans, nil })
	if code != 1 || !strings.Contains(out.String(), "unsupported schema version") || !strings.Contains(out.String(), "Next action:") || !strings.Contains(errOut.String(), "doctor found problems") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, &out, &errOut)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("diagnostics changed the plan")
	}
}
