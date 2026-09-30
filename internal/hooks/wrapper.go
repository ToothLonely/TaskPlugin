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
	var s strings.Builder
	s.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&s, "git_task_v1_binary=%s\n", quote(filepath.ToSlash(binary)))
	s.WriteString("git_task_v1_status=0\n")
	if event == "post-rewrite" {
		s.WriteString(`git_task_v1_input=$(mktemp "${TMPDIR:-/tmp}/git-task-rewrite.XXXXXX") || git_task_v1_input=
if [ -z "$git_task_v1_input" ]; then
    printf '%s\n' 'git-task: не удалось сохранить stdin post-rewrite; выполните git task sync.' >&2
fi
if [ -n "$git_task_v1_input" ]; then
    if ! cat > "$git_task_v1_input"; then
        rm -f -- "$git_task_v1_input"
        git_task_v1_input=
        printf '%s\n' 'git-task: не удалось прочитать stdin post-rewrite; выполните git task sync.' >&2
    fi
fi
`)
	}
	if enabled {
		_, body, _ := strings.Cut(string(original), "\n")
		s.WriteString("(\n")
		s.WriteString(body)
		s.WriteString("\n)")
		if event == "post-rewrite" {
			s.WriteString(" < \"${git_task_v1_input:-/dev/stdin}\"")
		}
		s.WriteString("\ngit_task_v1_status=$?\n")
	}
	s.WriteString("if [ -z \"${GIT_TASK_OPERATION:-}\" ] && [ -z \"${GIT_TASK_HOOK:-}\" ]; then\n")
	fmt.Fprintf(&s, "    GIT_TASK_HOOK=1 \"$git_task_v1_binary\" _hook %s \"$@\" < ", quote(event))
	if event == "post-rewrite" {
		s.WriteString("\"${git_task_v1_input:-/dev/null}\"")
	} else {
		s.WriteString("/dev/null")
	}
	s.WriteString(" || printf '%s\\n' 'git-task: tracking недоступен; Git-операция уже произошла; выполните git task sync.' >&2\nfi\n")
	if event == "post-rewrite" {
		s.WriteString("if [ -n \"$git_task_v1_input\" ]; then rm -f -- \"$git_task_v1_input\"; fi\n")
	}
	s.WriteString("exit \"$git_task_v1_status\"\n")
	return []byte(s.String())
}
