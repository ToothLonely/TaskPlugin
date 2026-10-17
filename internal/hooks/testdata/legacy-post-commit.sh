#!/bin/sh
git_task_v1_binary=@BINARY@
git_task_v1_status=0
if [ -z "${GIT_TASK_OPERATION:-}" ] && [ -z "${GIT_TASK_HOOK:-}" ]; then
    GIT_TASK_HOOK=1 "$git_task_v1_binary" _hook 'post-commit' "$@" < /dev/null || printf '%s\n' 'git-task: tracking недоступен; Git-операция уже произошла; выполните git task sync.' >&2
fi
exit "$git_task_v1_status"
