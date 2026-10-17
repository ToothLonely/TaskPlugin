package cli

import (
	"os"
	"strings"

	"git-task/internal/diagnostic"
)

func diagnosticLanguage() diagnostic.Language {
	return selectLanguage(os.Getenv, systemLanguage)
}

func selectLanguage(getenv func(string) string, system func() string) diagnostic.Language {
	locale := ""
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			locale = value
			break
		}
	}
	if locale == "" {
		locale = system()
	} else if baseLanguage(locale) != "c" && baseLanguage(locale) != "posix" {
		if preference := strings.TrimSpace(getenv("LANGUAGE")); preference != "" {
			locale, _, _ = strings.Cut(preference, ":")
		}
	}
	if baseLanguage(locale) == "ru" {
		return diagnostic.Russian
	}
	return diagnostic.English
}

func baseLanguage(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(locale, "_.@-"); i >= 0 {
		locale = locale[:i]
	}
	return locale
}
