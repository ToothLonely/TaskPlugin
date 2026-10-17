package hooks

import (
	"fmt"
	"path/filepath"
	"strings"
)

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func wrapper(event, binary string, original []byte, enabled bool) []byte {
	return versionedWrapper(event, binary, original, enabled, 2)
}

func versionedWrapper(event, binary string, original []byte, enabled bool, version int) []byte {
	saveInput := "git-task: could not save stdin post-rewrite; run git task sync."
	readInput := "git-task: could not read stdin post-rewrite; run git task sync."
	unavailable := "git-task: tracking unavailable; the Git operation has already occurred; run git task sync."
	if version == 1 {
		saveInput = "git-task: не удалось сохранить stdin post-rewrite; выполните git task sync."
		readInput = "git-task: не удалось прочитать stdin post-rewrite; выполните git task sync."
		unavailable = "git-task: tracking недоступен; Git-операция уже произошла; выполните git task sync."
	}
	var s strings.Builder
	s.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&s, "git_task_v1_binary=%s\n", quote(filepath.ToSlash(binary)))
	s.WriteString("git_task_v1_status=0\n")
	if event == "post-rewrite" || event == "reference-transaction" {
		fmt.Fprintf(&s, `git_task_v1_input=$(mktemp "${TMPDIR:-/tmp}/git-task-rewrite.XXXXXX") || git_task_v1_input=
if [ -z "$git_task_v1_input" ]; then
    printf '%%s\n' %s >&2
fi
if [ -n "$git_task_v1_input" ]; then
    if ! cat > "$git_task_v1_input"; then
        rm -f -- "$git_task_v1_input"
        git_task_v1_input=
        printf '%%s\n' %s >&2
    fi
fi
`, quote(saveInput), quote(readInput))
	}
	if enabled {
		_, body, _ := strings.Cut(string(original), "\n")
		s.WriteString("(\n")
		s.WriteString(body)
		s.WriteString("\n)")
		if event == "post-rewrite" || event == "reference-transaction" {
			s.WriteString(" < \"${git_task_v1_input:-/dev/stdin}\"")
		}
		s.WriteString("\ngit_task_v1_status=$?\n")
	}
	s.WriteString("if [ -z \"${GIT_TASK_OPERATION:-}\" ] && [ -z \"${GIT_TASK_HOOK:-}\" ]; then\n")
	fmt.Fprintf(&s, "    GIT_TASK_HOOK=1 \"$git_task_v1_binary\" _hook %s \"$@\" < ", quote(event))
	if event == "post-rewrite" || event == "reference-transaction" {
		s.WriteString("\"${git_task_v1_input:-/dev/null}\"")
	} else {
		s.WriteString("/dev/null")
	}
	s.WriteString(" || printf '%s\\n' " + quote(unavailable) + " >&2\nfi\n")
	if event == "post-rewrite" || event == "reference-transaction" {
		s.WriteString("if [ -n \"$git_task_v1_input\" ]; then rm -f -- \"$git_task_v1_input\"; fi\n")
	}
	s.WriteString("exit \"$git_task_v1_status\"\n")
	result := s.String()
	if event == "reference-transaction" {
		result = strings.ReplaceAll(result, "stdin post-rewrite", "stdin reference-transaction")
	}
	return []byte(result)
}
