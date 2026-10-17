package cli

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func systemLanguage() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages")
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(data))
	if !strings.HasPrefix(value, "(") || !strings.HasSuffix(value, ")") {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(value[1:len(value)-1]), ",")
	return strings.Trim(strings.TrimSpace(first), "\"")
}
