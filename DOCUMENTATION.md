# Git Task documentation

[English](#english) | [Русский](#russian)

<a id="english"></a>

[Quick setup and getting started](README.md#english).

Git Task is a standalone `git-task` program available as the external Git command `git task`.
Running `git-task start feature/search` directly is equivalent to `git task start feature/search`.

## Contents

- [Command syntax](#command-syntax).
- [Creating a plan](#creating-a-plan).
- [Viewing the plan and help](#viewing-the-plan-and-help).
- [Adding and editing tasks](#adding-and-editing-tasks).
- [Starting work and attaching a branch](#starting-work-and-attaching-a-branch).
- [Pausing, resuming, and starting another attempt](#pausing-resuming-and-starting-another-attempt).
- [Explicit completion and archiving](#explicit-completion-and-archiving).
- [Import and export](#import-and-export).
- [Fetching and publishing plan changes](#fetching-and-publishing-plan-changes).
- [Hooks, diagnostics, and recovery](#hooks-diagnostics-and-recovery).
- [Manual hook integration](#manual-hook-integration).
- [Updating and uninstalling](#updating-and-uninstalling).
- [Building from source](#building-from-source).
- [Merges on your own Git server](#merges-on-your-own-git-server).

## Command syntax

All user commands are listed below. `[...]` indicates an optional parameter;
replace `<...>` with your own value. Do not type the brackets.
Alternatives separated by `/` are mutually exclusive. For `move`, one is required.
Every user command supports `--help`.

| Command | Arguments after the command name | Purpose |
|---|---|---|
| `help` | `[command]` | Help for a command or the full command list |
| `version` | — | Program version |
| `init` | `[--apply / --target <branch>]` | Create or apply a template, or create an empty ready plan |
| `status` | `[--json]` | Reconcile with Git and show the plan |
| `show` | `--id <task-id>` | Show a task and its attempt history without reconciliation |
| `sync` | — | Reconcile with local Git |
| `add` | `<title> [--description <text>] [--after <id> / --before <id> / --end]` | Create a todo task |
| `edit` | `--id <task-id> [--title <title>] [--description <text>]` | Edit a task or open an editor |
| `move` | `--id <task-id> (--after <id> / --before <id> / --end)` | Change task order |
| `start` | `<branch> [--id <id> / --title <title> / --new <title> / --select] [--from <ref>] [--again]` | Create a branch and start an attempt |
| `attach` | `<branch> --id <task-id> [--rebind [--attempt <attempt-id>]]` | Attach an existing branch |
| `pause` | `--id <task-id> [--attempt <attempt-id>]` | Pause an attempt |
| `resume` | `--id <task-id> [--attempt <attempt-id>]` | Resume an attempt and switch branches |
| `complete` | `--id <task-id> [--attempt <attempt-id>] [--commit <oid>] [--yes]` | Explicitly complete an attempt |
| `archive` | `--id <task-id>` | Archive a task |
| `import` | `<path> [--format markdown / --format json] [--yes]` | Import into an empty plan |
| `export` | `[--format markdown / --format json] [--output <path>]` | Export a snapshot |
| `team connect` | `--remote <name>` | Connect to a shared plan |
| `team fetch` | — | Fetch and reconcile the shared plan |
| `team publish` | — | Reconcile and publish the queue |
| `team reconcile` | `[--before <oid> --after <oid>]` | Process a server event or retry a saved one |
| `hooks install` | — | Enable local tracking |
| `hooks uninstall` | — | Remove hook integration |
| `doctor` | `[--repair recover-start / restore-backup / unlock] [--yes]` | Diagnose problems and explicitly recover |

`git task`, `git task --help`, and `git task help` show the full command list;
`git task --version` is equivalent to `git task version`. Help and version commands
work outside a repository. Running `git-task version` directly does not require Git itself.

Exit codes: `0` — success, `1` — execution error, `2` — argument error,
`130` — cancellation or timeout. A publication failure after a successful local write
may produce a warning while preserving the queue.

## Creating a plan

```text
git task init
git task init --apply
git task init --target master
```

With no flags, `init` creates `.git-task/plan.json` with just two fields:
`"target_branch": ""` and `"tasks": []`. Fill in the target branch and task array
using the [README example](README.md#2-set-up-your-project-and-fill-in-planjson),
save the file, and run `git task init --apply`. This command explicitly signals
that editing is complete: the program does not watch your editor or apply the
template automatically. Plan commands require setup to be finished before use.

`--apply` validates the entire template and generates a ready plan. Repeating `init`
or `init --apply` preserves a ready file, its IDs, and its history. Repeating plain
`init` also preserves a template that has not yet been applied. A damaged or foreign
file causes an error. The original filled-in template is saved as
`.git-task/plan.template.json`; `plan.backup.json` remains the backup of the previous
ready plan. Do not replace a ready plan with a new minimal template: that loses its
references and history. Applying such a replacement is rejected when existing
history files are detected alongside it.

For automation, `init --target <branch>` creates an empty ready plan immediately:
you can then use `add` or `import` without manual editing. It does not convert an
existing template; use `--apply` with the target set in the file. `--target` and
`--apply` are mutually exclusive. Repeating `--target` requires the same target.
The directory is excluded through local `info/exclude`; code files and `.gitignore`
are not changed. In a repository with no commits, `init` warns that `start` will
still need an existing starting point.

Only schema 3 is supported. Plans using schemas 1–2 are rejected without changing
the source file; there is no automatic upgrade or migration command. JSON import
also accepts only the current schema. This applies to ready plans, not the minimal
initial template converted by `init --apply`.

## Commands and examples

`<task-id>` below is a full task ID from `git task status --json` or its unique prefix.
A task's position in the text list is not its ID. `<attempt-id>` is an attempt ID from
`git task show --id <task-id>`.

Short IDs are supported by `--id` in `start`, `attach`, `show`, `edit`, `move`,
`pause`, `resume`, `complete`, and `archive`, and by `--before`/`--after` in `add` and `move`.
For example, you can use `--id 9f86` for `9f86d081884c4d659a2feaa0c55ad015`
if no other task starts with that prefix. There is no fixed abbreviation length.
Matching is case-sensitive; an exact full match is sufficient even if other IDs
start with it. If several IDs match, the command lists them all and refuses to proceed
before changing the plan or Git. The search includes all tasks, including completed
and archived ones.

Abbreviations are used only for selection: the plan file, operation journal, and
references retain full IDs. `--attempt` still requires the exact full attempt ID.

### Task IDs and filling in the template manually

The initial template requires `target_branch` and a `tasks` array; each task requires
only a nonempty `title`. The target branch must be nonempty when applying the template;
an empty `tasks: []` is allowed. `description` and a custom string `id` are optional.
IDs must be nonempty, unique, and free of control characters. Unknown fields, `null`,
duplicate JSON keys, and invalid UTF-8 are rejected without overwriting the file.

`init --apply` adds missing IDs, the initial `todo` status, counters, and `order`
containing full IDs in the order tasks appear in the `tasks` array. A subsequent
`add` continues this order by default. Once applied, `order` determines the sequence;
rearranging objects in `tasks` alone does not change it. Use `move` and
`--before`/`--after`/`--end` so the program maintains `order` automatically.
Fill in the file manually before starting work; use the CLI for later changes
to preserve history and reconcile the plan with other participants.

`add "title"` and `start <branch> --new "title"` use the same add operation.
An ID is generated from 16 cryptographically random bytes and stored as a string
of 32 lowercase hexadecimal characters `0–9`, `a–f`, with no prefix or hyphens.
Example: `9f86d081884c4d659a2feaa0c55ad015`. Titles may repeat; IDs may not.
The ID stays the same when a task is renamed, moved, or given another attempt.

Leave `id` out of the template to generate all IDs consistently.
Custom IDs such as `001` or `task-001` are also valid and are not renamed.
Mixed formats are valid: IDs in the plan are compared as exact strings,
while user commands allow a unique prefix. Sequential IDs can collide when
tasks are added independently in different clones; automatic generation does
not depend on a local counter.

`--id` and `--title` in `start` select an existing task; `--new` takes a new task's title.
The combination `--new --id "001" --title "title"` is not supported.
You do not need to enter additional fields when creating a task: the program fills
in the ID and internal values. `revision` tracks changes to a plan or task;
`last_event` counts recorded attempt completions. Both are initialized to 0
when applying the template.

### The format and schema_version fields

`format: "git-task"` is a constant document-type marker. It tells the program that
the file is a Git Task plan rather than another kind of JSON. An incorrect marker
causes processing and overwriting to be refused. It does not protect against malicious
data: the structure and all references are validated separately.

`schema_version: 3` is the plan structure version: the fields and rules the program
can read. It allows incompatible schemas to be explicitly rejected. Only version 3
is currently accepted. `format` identifies the document type; `schema_version`
identifies the version of its structure. `init --apply` adds both fields; users do not
enter them in the initial template. Do not change these internal values manually.

### The attempts structure

A `todo` task may have no `attempts` array before work starts.
A successful `start` adds an attempt to this array and saves it in plan.json.
With `start --new`, the task and attempt are created in one operation.
A missing plan or a branch-creation failure does not record a new attempt.
`show --id <task-id>` displays all known attempts.

An example record after starting work, with placeholder Git commits and author:

```json
{
  "id": "b073ba5c76d14941a2d2c06e0ab00ba8",
  "author": "Developer <developer@example.invalid>",
  "status": "active",
  "branch": "feature/search",
  "original_branch": "feature/search",
  "target_branch": "main",
  "base_commit": "1111111111111111111111111111111111111111",
  "started_at": "2026-10-08T10:00:00Z",
  "observation": {
    "tip": "1111111111111111111111111111111111111111",
    "target_commit": "1111111111111111111111111111111111111111"
  }
}
```

| Attempt field | Meaning |
|---|---|
| `id` | Stable attempt ID; distinct from the task ID |
| `author` | Author from the Git identity |
| `status` | `active`, `paused`, or `done` |
| `branch` | Currently attached branch |
| `original_branch` | Branch when the attempt started |
| `target_branch` | Branch that should receive the work |
| `base_commit` | Commit the branch starts from |
| `started_at` | Start time in RFC 3339 format |
| `observation` | Local Git state for later reconciliation |
| `rebindings` | History of binding changes through `attach --rebind`; appears after a change |
| `completion` | Evidence or basis for completion; appears after completion |

`observation` contains `tip`, `target_commit`, and, when available, `work_commit`,
`branch_log`, and `target_log`. These local values are not included in shared JSON export.
Each `rebindings` entry contains `from`, `to`, `base_commit`, and `observed_at`.
`completion` requires `event`, `source`, and `target_branch`; it may also contain
`completed_at`, `observed_at`, `commit`, `merge_kind`, `work_commit`, and `target_before`.
Some fields are optional: for example, an imported task's completion has no fabricated
Git branch or start time. The CLI maintains this data itself.

### Viewing the plan and help

```text
git task status
git task status --json
git task show --id <task-id>
git task sync
git task help
git task help start
git task version
```

`status` reconciles the plan with local Git and shows progress. `show` reads a task,
its description, and the history of all attempts without reconciliation or writes.
`sync` reconciles locally without printing the full plan. These commands do not
access the network. `status --json` outputs a schema 3 plan with task and attempt IDs.
`show` also works for archived tasks. Use `help team` and `help hooks` for subcommand help.

### Adding and editing tasks

```text
git task add "Add filters" --description "Filter by date and category"
git task add "Update documentation" --after <task-id>
git task edit --id <task-id> --title "Add a date filter"
git task edit --id <task-id> --description "Work description"
git task edit --id <task-id>
git task move --id <task-id> --before <other-task-id>
git task move --id <task-id> --end
```

Without change flags, `edit` opens the title and description in the configured
Git editor. `move` changes task order; specify exactly one option:
`--before`, `--after`, or `--end`. The task's ID and history are preserved.
For `add`, these flags are optional and set the new task's position; its ID is
assigned automatically. An empty `edit --description ""` clears the description.

The editor is chosen in this order: `GIT_EDITOR` → `core.editor` → `VISUAL` → `EDITOR`.
A graphical editor needs a wait mode, such as `code --wait`.
Quote paths containing spaces. The editor command is parsed as a program name
with arguments; variable expansion and shell expressions are not evaluated.
A temporary JSON file with two string fields, `title` and `description`, is edited.
On an error or conflict, the CLI reports where the edited result was saved.

### Starting work and attaching a branch

```text
git task start feature/search
git task start feature/search --title "Add a search screen"
git task start feature/search --id <task-id>
git task start feature/search --select
git task start feature/new --new "New task"
git task start feature/search --id <task-id> --from release
git task attach existing-branch --id <task-id>
```

Each line is a separate way to start. Without a selector, the first `todo` is chosen.
`--title` looks for an exact, unique title; use `--id` when titles are duplicated.
`--select` opens a menu in an interactive terminal. `--from` changes the new branch's
starting point while keeping the completion target.

The branch name is required, is not generated automatically, and must be available.
Creating a branch with plain `git switch -c` does not assign a task. For an existing
branch, use `attach`, which does not switch the working copy.
`start` requires a plan, a clean working tree, and a normal HEAD. Branch-creation
failure does not change task status. The `-b` flag is not supported.
`attach` creates an attempt for `todo`/`active`/`paused`; a `done` task needs `start --again`.
A branch occupied by another task is rejected; repeating a confirmed binding does
not create a duplicate.

### Pausing, resuming, and starting another attempt

```text
git task pause --id <task-id>
git task resume --id <task-id>
git task pause --id <task-id> --attempt <attempt-id>
git task resume --id <task-id> --attempt <attempt-id>
git task start feature/search-v2 --id <task-id> --again
git task attach replacement-branch --id <task-id> --rebind --attempt <attempt-id>
```

`pause` preserves the attempt and its branch binding. `resume` continues the attempt
and switches to its branch. If the branch is lost, `attach --rebind` binds the same
attempt to another existing local branch.

`--again` starts another attempt for a completed task while preserving its history.
It requires explicit selection through `--id` or `--title`. `--attempt` in `attach`
is allowed only with `--rebind`. `resume` requires a clean working tree and an existing
attached branch. Repeating `pause` for a paused attempt does not change the plan.
If there are multiple attempts and the choice is ambiguous, specify `--attempt`.
Each attempt has its own ID, author, and status. A task becomes `done` when all known
attempts are complete and the set is nonempty. A late-arriving unfinished attempt
can return the task to `active`.

### Explicit completion and archiving

```text
git task complete --id <task-id>
git task complete --id <task-id> --attempt <attempt-id> --commit <commit-oid> --yes
git task archive --id <task-id>
```

`complete` explicitly marks the selected attempt complete with a `manual` basis.
If supplied, `--commit` must be included in the target branch. Without `--yes`,
an interactive terminal is required for confirmation. The command does not check code quality.

`archive` removes a task from the normal list while preserving its history, code,
and branch. There is no unarchive command yet. An unfinished attempt is frozen,
and its branch becomes available for `attach`.

### Import and export

To add many tasks at once, create a UTF-8 `plan.md` file in your project
(you can name it `PLAN.md`) with a flat Markdown checklist:

```markdown
# Project plan

## Search
- [ ] Add a search screen
- [ ] Add filters

## Preparation
- [x] Create the repository
```

`- [ ]` means a new task; `- [x]` or `- [X]` means a completed one. Each item takes
one unindented line; line order determines task order. Headings with `#` and blank
lines are allowed and do not become tasks. Nested lists, separate descriptions,
tables, regular paragraphs, and code blocks are not supported in the import file.
Copy the example's contents into the file without the surrounding triple-backtick lines.

Before adding tasks through `add`, run:

```text
git task init --target main
git task import plan.md --yes
git task status
```

All items are added in one import, with IDs assigned automatically.
`--yes` confirms the import without prompting; omit it for a preview with interactive
confirmation. The filename is arbitrary and is passed explicitly: the CLI does not
look for `PLAN.md` automatically. Import requires an empty plan; later changes to
the Markdown file are not synchronized with the CLI. Use `git task add` for later tasks.

```text
git task import tasks.md
git task import snapshot.json --format json --yes
git task export
git task export --format markdown --output tasks-copy.md
git task export --format json --output snapshot.json
```

Export without `--output` writes to the terminal. An existing output file is not
overwritten. JSON preserves tasks and attempts, but not the local queue, observations,
or connection settings; preserving those requires a backup of the working plan.
Export does not reconcile state: run `sync` first for an up-to-date snapshot.
Always specify `--format json` when importing JSON.

### Fetching and publishing plan changes

```text
git task team connect --remote origin
git task team fetch
git task team publish
```

After `connect`, task changes are automatically submitted for publication.
`team fetch` retrieves the shared plan; `team publish` reconciles it with the local
queue and sends changes. Regular `status`, `sync`, and hooks work locally;
they do not publish the queue on their own.

If the network or permissions fail, already saved work remains local and the CLI
prints a warning. Once the issue is resolved, run `team publish`; there is no need
to repeat `start` or `complete` to send changes. Publication makes up to three attempts
within 30 seconds. There is no automatic background retry process.

Plain `git fetch` can retrieve the service branch if the remote's refspec allows it;
`team fetch` retrieves it explicitly. Merging code on a Git hosting service does not
invoke local hooks. Setup for your own Git server is described
[below](#merges-on-your-own-git-server).

### Hooks, diagnostics, and recovery

```text
git task hooks install
git task hooks uninstall
git task doctor
git task doctor --repair recover-start --yes
git task doctor --repair restore-backup --yes
git task doctor --repair unlock --yes
```

`hooks uninstall` removes integration while preserving the plan and other handlers.
Hooks handle `post-commit`, `post-merge`, `post-checkout`, `post-rewrite`, and
`reference-transaction`, with no prompts or network access. They require `sh`,
`mktemp`, `cat`, and `rm`; on Windows, these come with Git for Windows.
The binary must stay at a permanent path. Without repair, `doctor` only reads state.
Choose a recovery action based on its diagnostics: `recover-start` completes an
interrupted operation, `restore-backup` restores the last valid copy, and `unlock`
removes a lock whose owner has been confirmed absent. A backup may lack recent
changes; do not delete the plan, queue, or journals to bypass an error.

The CLI requires a normal full repository with one working tree;
bare repositories, shallow or partial clones, and additional worktrees are not supported.

### Manual hook integration

`_hook <event> [Git arguments]` is an internal entry point for hooks, not an everyday
command. Use it only for the manual integration described below.

Automatic installation preserves supported regular shell hooks.
A shared `core.hooksPath`, third-party manager, different interpreter, or modified
wrapper requires manual integration. The CLI preserves other files when it refuses
installation. For a manager, use its extension mechanism; change a shared directory
only with its owner's permission. You can leave integration disabled and run
`git task sync` yourself.

For a regular shell hook, after its original logic, save the exit code and add a call
using the absolute path to the installed binary:

```sh
git_task_original_status=$?
if [ -z "${GIT_TASK_OPERATION:-}" ] && [ -z "${GIT_TASK_HOOK:-}" ]; then
    GIT_TASK_HOOK=1 '/path/to/git-task' _hook post-commit < /dev/null ||
        printf '%s\n' 'git-task: run git task sync.' >&2
fi
exit "$git_task_original_status"
```

On Windows, use forward slashes and the path to `git-task.exe`. For `post-merge`
and `post-checkout`, pass the actual event name and `"$@"`.
For `post-rewrite` and `reference-transaction`, each handler needs a copy of the
original stdin. If the manager cannot provide this, use a separate `git task sync`;
stdin that has already been consumed cannot be passed again.

## Updating and uninstalling

Initial installation is described in the [README](README.md#english). On Linux
without a graphical interface, install the downloaded package with
`sudo apt install ./<file>.deb` or `sudo dnf install ./<file>.rpm`.
Windows/macOS installers are currently unsigned. Portable archives are available
for Intel macOS; the current macOS CI checks ARM64.

If Windows reports `git: 'task' is not a git command`, fully restart your terminal
application or IDE. A new tab alone may retain the old PATH.
Check the installed file by calling it directly from PowerShell:

```powershell
& "$env:LOCALAPPDATA\Programs\GitTask\bin\git-task.exe" version
```

If the version is printed, installation succeeded. If the file is missing, run
the MSI installer again and wait for successful completion.

To update, download and run the new version's installer. The plugin path stays
the same; you do not need to repeat `init` in your projects. Then open a new terminal
and check `git task version` and `git task status`. The installer does not change
project plans or queues.

Before uninstalling, run `git task hooks uninstall` in projects where you enabled hooks.
On Windows, remove Git Task through the installed applications list.
On Debian/Ubuntu, use `sudo apt remove git-task`; on RPM-based distributions,
use `sudo dnf remove git-task`. On macOS, remove `/usr/local/bin/git-task`,
`/etc/paths.d/git-task`, and `/usr/local/share/doc/git-task`, then forget the receipt
with `sudo pkgutil --forget io.github.toothlonely.git-task`.
The `.git-task` directories in your projects are preserved.

If you previously used the old installation script for an individual project,
remove its hooks before switching to the shared installer, then run
`git config --local --unset alias.task` and
`git config --local --unset git-task.install-path` in that project.
You can install hooks again after installing the new plugin.

## Building from source

Extract a portable archive into a permanent directory and add it to PATH.
With Go 1.26.0+, you can build the CLI yourself from a clone of this repository.
On Windows, in PowerShell:

```powershell
go build -o bin/git-task.exe ./cmd/git-task
$gitTaskBin = Join-Path $PWD 'bin'
$env:PATH = $gitTaskBin + [IO.Path]::PathSeparator + $env:PATH
```

On Linux/macOS:

```sh
go build -o bin/git-task ./cmd/git-task
export PATH="$PWD/bin:$PATH"
```

These examples update PATH only in the current terminal. Keep the built binary
at a permanent path, especially after installing hooks.

## Merges on your own Git server

For a regular Git server with hook access, a
[post-receive example](scripts/git-task-post-receive.sample) is provided.
An administrator installs it while preserving the existing handler.
The example does not set up integration with GitHub/GitLab SaaS; those hosting
services need a separate server trigger.

Create a separate regular clone for the worker, configure its local Git identity,
and run `git task init --target <branch>` and `git task team connect --remote <name>`.
The server process needs `GIT_TASK_WORKER` set to that clone's path,
`GIT_TASK_BINARY` to the binary's absolute path, and `GIT_TASK_TARGET` to the target
branch (default: `main`). The worker needs permission to fetch code and publish the plan.

After the target branch is updated, the handler runs:

```text
git task team reconcile --before <old-commit-oid> --after <new-commit-oid>
```

Retry a saved event or queue with `git task team reconcile` without flags.
If the initial write failed, use the exact command with OIDs from the server
diagnostic output. Keep the attempt's branch until the event has been processed.
Creating or deleting the target, rewritten history, squash, and cherry-pick do not
cause automatic completion. Processing is synchronous and can delay a push by up to
30 seconds; a plan publication failure does not undo a successful code push.

---

<a id="russian"></a>

# Документация Git Task

[Быстрая настройка и начало работы](README.md#russian).

Git Task — отдельная программа `git-task`, доступная как внешняя команда Git `git task`.
Прямой вызов `git-task start feature/search` эквивалентен `git task start feature/search`.

## Содержание

- [Синтаксис всех команд](#синтаксис-всех-команд).
- [Создание плана](#создание-плана).
- [Просмотр плана и справки](#просмотр-плана-и-справки).
- [Добавление и изменение задач](#добавление-и-изменение-задач).
- [Начало работы и привязка ветки](#начало-работы-и-привязка-ветки).
- [Пауза, продолжение и новый подход](#пауза-продолжение-и-новый-подход).
- [Явное завершение и архивирование](#явное-завершение-и-архивирование).
- [Импорт и экспорт](#импорт-и-экспорт).
- [Получение и публикация изменений плана](#получение-и-публикация-изменений-плана).
- [Hooks, диагностика и восстановление](#hooks-диагностика-и-восстановление).
- [Ручное подключение hooks](#ручное-подключение-hooks).
- [Обновление и удаление](#обновление-и-удаление).
- [Сборка из исходников](#сборка-из-исходников).
- [Слияние на собственном Git-сервере](#слияние-на-собственном-git-сервере).

## Синтаксис всех команд

Ниже указаны все пользовательские команды. `[...]` означает необязательный
параметр; `<...>` замените своим значением. Скобки вводить не нужно.
Разделённые `/` варианты взаимоисключаются. Для `move` один вариант обязателен.
Каждая пользовательская команда поддерживает `--help`.

| Команда | Аргументы после имени команды | Назначение |
|---|---|---|
| `help` | `[command]` | Справка по команде или общий список |
| `version` | — | Версия программы |
| `init` | `[--apply / --target <branch>]` | Создать шаблон, применить его или создать пустой готовый план |
| `status` | `[--json]` | Сверить с Git и показать план |
| `show` | `--id <task-id>` | Задача и история подходов без сверки |
| `sync` | — | Локальная сверка с Git |
| `add` | `<title> [--description <text>] [--after <id> / --before <id> / --end]` | Новая задача todo |
| `edit` | `--id <task-id> [--title <title>] [--description <text>]` | Изменить задачу или открыть редактор |
| `move` | `--id <task-id> (--after <id> / --before <id> / --end)` | Изменить порядок задач |
| `start` | `<branch> [--id <id> / --title <title> / --new <title> / --select] [--from <ref>] [--again]` | Создать ветку и начать подход |
| `attach` | `<branch> --id <task-id> [--rebind [--attempt <attempt-id>]]` | Привязать существующую ветку |
| `pause` | `--id <task-id> [--attempt <attempt-id>]` | Приостановить подход |
| `resume` | `--id <task-id> [--attempt <attempt-id>]` | Продолжить подход с переключением ветки |
| `complete` | `--id <task-id> [--attempt <attempt-id>] [--commit <oid>] [--yes]` | Явно завершить подход |
| `archive` | `--id <task-id>` | Архивировать задачу |
| `import` | `<path> [--format markdown / --format json] [--yes]` | Импорт в пустой план |
| `export` | `[--format markdown / --format json] [--output <path>]` | Экспорт снимка |
| `team connect` | `--remote <name>` | Подключить общий план |
| `team fetch` | — | Получить и согласовать общий план |
| `team publish` | — | Согласовать и опубликовать очередь |
| `team reconcile` | `[--before <oid> --after <oid>]` | Обработать серверное событие или повторить сохранённое |
| `hooks install` | — | Подключить локальное отслеживание |
| `hooks uninstall` | — | Снять интеграцию hooks |
| `doctor` | `[--repair recover-start / restore-backup / unlock] [--yes]` | Диагностика и явное восстановление |

Вызовы `git task`, `git task --help` и `git task help` показывают общий список;
`git task --version` равнозначен `git task version`. Справка и версия доступны
вне репозитория. Для прямого `git-task version` сам Git не требуется.

Коды выхода: `0` — успех, `1` — ошибка выполнения, `2` — ошибка аргументов,
`130` — отмена или превышение срока операции. Отказ публикации после успешной
локальной записи может быть предупреждением с сохранённой очередью.

## Создание плана

```text
git task init
git task init --apply
git task init --target master
```

Без флагов `init` создаёт `.git-task/plan.json` только с двумя полями:
`"target_branch": ""` и `"tasks": []`. Заполните целевую ветку и массив задач
по [примеру README](README.md#2-подключите-проект-и-заполните-planjson), сохраните
файл и выполните `git task init --apply`. Команда служит явным сигналом завершения
редактирования: программа не следит за редактором и не преобразует шаблон сама.
До применения команды работы с планом требуют закончить настройку.

`--apply` проверяет весь шаблон и генерирует готовый план. Повторные `init` и
`init --apply` сохраняют уже готовый файл, ID и историю. Повторный обычный `init`
также сохраняет ещё не применённый шаблон. Повреждённый или чужой файл даёт ошибку.
Исходный заполненный шаблон сохраняется в `.git-task/plan.template.json`;
`plan.backup.json` остаётся резервной копией предыдущей версии готового плана.
Не заменяйте готовый план новым минимальным шаблоном: это потеря его связей и истории;
при обнаруженной соседней истории применение такой замены отклоняется.

Для автоматизации `init --target <branch>` сразу создаёт готовый пустой план:
после него можно использовать `add` или `import` без ручного редактирования.
Он не преобразует существующий шаблон; используйте `--apply` с целью из файла.
Флаги `--target` и `--apply` взаимоисключающие. Повторный `--target` требует прежнюю цель.
Каталог исключается через локальный `info/exclude`;
файлы кода и `.gitignore` не меняются. В репозитории без коммитов `init` выдаёт
предупреждение: для `start` ещё потребуется существующее основание.

Поддерживается только схема 3. Планы схем 1–2 отклоняются без изменения исходника;
автоматического перехода и команды миграции нет. JSON-импорт тоже принимает
только текущую схему. Это правило относится к готовым планам, а не к минимальному
начальному шаблону, который преобразует `init --apply`.

## Команды и примеры

`<task-id>` ниже — полный ID задачи из `git task status --json` или его уникальный префикс.
Позиция задачи в текстовом списке не является ID. `<attempt-id>` — ID подхода из
`git task show --id <task-id>`.

Короткие ID поддерживаются в `--id` у `start`, `attach`, `show`, `edit`, `move`,
`pause`, `resume`, `complete`, `archive`, а также в `--before`/`--after` у `add` и `move`.
Например, для `9f86d081884c4d659a2feaa0c55ad015` можно ввести `--id 9f86`,
если другой задачи с этим началом нет. Фиксированной длины сокращения нет.
Сравнение учитывает регистр; полного точного совпадения достаточно даже при наличии
других ID с тем же началом. При нескольких совпадениях команда выводит все
совпавшие ID и отказывает до изменений плана или Git. В поиске участвуют
все задачи, включая завершённые и архивные.

Сокращения используются только для выбора: файл плана, журнал операций и связи
содержат полный ID. `--attempt` по-прежнему требует полный точный ID подхода.

### ID задач и ручное заполнение шаблона

В начальном шаблоне обязательны `target_branch` и массив `tasks`; у каждой задачи
обязателен только непустой `title`. Целевая ветка при применении должна быть непустой;
пустой `tasks: []` допустим. Необязательны `description` и собственный строковый `id`.
ID должны быть непустыми, уникальными и без управляющих символов. Неизвестные поля,
`null`, повторные ключи JSON и некорректный UTF-8 отклоняются без перезаписи файла.

`init --apply` добавляет отсутствующие ID, начальный статус `todo`, счётчики и
`order` с полными ID в порядке записи задач в массиве `tasks`. Последующий `add`
по умолчанию продолжает этот порядок. После применения порядок определяет `order`;
одна перестановка объектов в `tasks` не меняет его. Используйте `move` и
`--before`/`--after`/`--end`, чтобы программа поддерживала `order` автоматически.
Ручное заполнение выполняйте до начала работы; последующие изменения делайте
через CLI, чтобы сохранить историю и согласовать план с другими участниками.

`add "title"` и `start <branch> --new "title"` вызывают одну операцию добавления.
ID генерируется из 16 криптографически случайных байт и записывается как строка
из 32 строчных шестнадцатеричных символов `0–9`, `a–f`, без префикса и дефисов.
Пример: `9f86d081884c4d659a2feaa0c55ad015`. Title может повторяться, ID — нет.
Он сохраняется после переименования, перемещения и новых подходов.

Оставьте `id` в шаблоне отсутствующим, чтобы все ID генерировались единообразно.
Собственные ID вроде `001` или `task-001` также допустимы
и не переименовываются. Смешанный формат корректен: в плане ID сравниваются
как точные строки, а пользовательские команды допускают уникальный префикс.
Последовательные ID могут столкнуться при независимом добавлении в разных клонах;
автоматическая генерация не зависит от локального счётчика.

`--id` и `--title` у `start` выбирают существующую задачу; `--new` принимает
название новой. Сочетание `--new --id "001" --title "title"` не поддерживается.
Дополнительные поля при создании вводить не нужно: ID и служебные значения
заполняются программой. `revision` отмечает изменения плана/задачи, `last_event`
считает зарегистрированные завершения подходов; при применении они создаются равными 0.

### Поля format и schema_version

`format: "git-task"` — постоянная метка вида документа. Она сообщает программе,
что файл является планом Git Task, а не другим JSON. Неверная метка вызывает отказ
от обработки и перезаписи. Это не защита от вредоносных данных: структура и все
связи проверяются отдельно.

`schema_version: 3` — версия структуры плана: какие поля и правила программа
умеет читать. Она позволяет явно отклонить несовместимую схему. Сейчас принимается
только версия 3. `format` отвечает «какой это документ», `schema_version` —
«какая версия его устройства». Оба поля добавляет `init --apply`; в исходном
шаблоне пользователь их не вводит. Эти служебные значения вручную не меняйте.

### Структура attempts

До начала работы у задачи `todo` массив `attempts` может отсутствовать.
При успешном `start` программа добавляет подход в этот массив и сохраняет его
в plan.json. Для `start --new` задача и подход создаются одной операцией.
Проверка отсутствующего плана или ошибка создания ветки не записывают новый подход.
`show --id <task-id>` показывает все известные подходы.

Пример записи после начала работы, с условными значениями Git-коммитов и автора:

```json
{
  "id": "b073ba5c76d14941a2d2c06e0ab00ba8",
  "author": "Developer <developer@example.invalid>",
  "status": "active",
  "branch": "feature/search",
  "original_branch": "feature/search",
  "target_branch": "main",
  "base_commit": "1111111111111111111111111111111111111111",
  "started_at": "2026-10-08T10:00:00Z",
  "observation": {
    "tip": "1111111111111111111111111111111111111111",
    "target_commit": "1111111111111111111111111111111111111111"
  }
}
```

| Поле подхода | Значение |
|---|---|
| `id` | Устойчивый ID подхода; отличается от ID задачи |
| `author` | Автор из Git identity |
| `status` | `active`, `paused` или `done` |
| `branch` | Текущая связанная ветка |
| `original_branch` | Ветка при начале подхода |
| `target_branch` | Ветка, в которую должна попасть работа |
| `base_commit` | Коммит основания ветки |
| `started_at` | Время начала в RFC 3339 |
| `observation` | Локальное состояние Git для последующей сверки |
| `rebindings` | История замены связи через `attach --rebind`; появляется при замене |
| `completion` | Доказательство или основание завершения; появляется после завершения |

`observation` содержит `tip`, `target_commit` и, при наличии, `work_commit`,
`branch_log`, `target_log`. Эти локальные данные не входят в общий JSON-экспорт.
Каждая запись `rebindings` содержит `from`, `to`, `base_commit`, `observed_at`.
`completion` содержит обязательные `event`, `source`, `target_branch` и возможные
`completed_at`, `observed_at`, `commit`, `merge_kind`, `work_commit`, `target_before`.
Часть полей необязательна: у завершения импортированной задачи, например,
нет выдуманной Git-ветки и времени начала. Эти данные CLI ведёт самостоятельно.

### Просмотр плана и справки

```text
git task status
git task status --json
git task show --id <task-id>
git task sync
git task help
git task help start
git task version
```

`status` сверяет план с локальным Git и показывает прогресс. `show` читает задачу,
описание и историю всех подходов без сверки и записи. `sync` выполняет локальную
сверку без вывода полного плана. Эти команды не обращаются в сеть.
`status --json` выводит план схемы 3 с ID задач и подходов. `show` доступен
и для архивных задач. Справку по подкомандам смотрите через `help team` и `help hooks`.

### Добавление и изменение задач

```text
git task add "Добавить фильтры" --description "Фильтр по дате и категории"
git task add "Обновить документацию" --after <task-id>
git task edit --id <task-id> --title "Добавить фильтр по дате"
git task edit --id <task-id> --description "Описание работы"
git task edit --id <task-id>
git task move --id <task-id> --before <other-task-id>
git task move --id <task-id> --end
```

`edit` без флагов изменения открывает название и описание в настроенном
Git-редакторе. `move` меняет порядок задач; укажите ровно один вариант:
`--before`, `--after` или `--end`. ID и история задачи сохраняются.
У `add` эти же флаги необязательны и задают место новой задачи; ID
назначается автоматически. Пустое `edit --description ""` очищает описание.

Редактор выбирается в порядке `GIT_EDITOR` → `core.editor` → `VISUAL` → `EDITOR`.
Для графического редактора нужен режим ожидания, например `code --wait`.
Путь с пробелами заключайте в кавычки. Команда редактора разбирается как имя
программы с аргументами; подстановки переменных и shell-выражения не выполняются.
Редактируется временный JSON с двумя строками `title` и `description`.
При ошибке или конфликте CLI сообщает путь к сохранённому результату.

### Начало работы и привязка ветки

```text
git task start feature/search
git task start feature/search --title "Добавить экран поиска"
git task start feature/search --id <task-id>
git task start feature/search --select
git task start feature/new --new "Новая задача"
git task start feature/search --id <task-id> --from release
git task attach existing-branch --id <task-id>
```

Каждая строка — отдельный вариант запуска. Без селектора выбирается первый
`todo`. `--title` ищет точное уникальное название; при одинаковых названиях
используйте `--id`. `--select` открывает меню в интерактивном терминале.
`--from` задаёт другое основание новой ветки, сохраняя цель завершения.

Имя ветки обязательно, автоматически не генерируется и должно быть свободным.
Создание ветки через обычный `git switch -c` не назначает задачу — для уже
существующей ветки есть `attach`, который не переключает рабочую копию.
Для `start` нужны план, чистое рабочее дерево и обычный HEAD. Ошибка создания
ветки не меняет статус задачи. Флаг `-b` не поддерживается.
`attach` создаёт подход к `todo`/`active`/`paused`; для `done` нужен `start --again`.
Ветка, занятая другой задачей, запрещена; повтор подтверждённой связи не создаёт дубль.

### Пауза, продолжение и новый подход

```text
git task pause --id <task-id>
git task resume --id <task-id>
git task pause --id <task-id> --attempt <attempt-id>
git task resume --id <task-id> --attempt <attempt-id>
git task start feature/search-v2 --id <task-id> --again
git task attach replacement-branch --id <task-id> --rebind --attempt <attempt-id>
```

`pause` сохраняет подход и связь с веткой. `resume` продолжает его и переключается
на связанную ветку. Если ветка потеряна, `attach --rebind` позволяет связать
тот же подход с другой существующей локальной веткой.

`--again` начинает новый подход к уже завершённой задаче, сохраняя историю.
Он требует явного выбора через `--id` или `--title`. `--attempt` у `attach`
разрешён только с `--rebind`. Для `resume` нужны чистое дерево и существующая
связанная ветка. Повтор `pause` для приостановленного подхода не меняет план.
Если подходов несколько и выбор неоднозначен, укажите `--attempt`.
Каждый подход имеет собственные ID, автора и статус. Задача становится `done`,
когда все известные подходы завершены и их набор непустой. Поздно полученный
незавершённый подход может вернуть задачу в `active`.

### Явное завершение и архивирование

```text
git task complete --id <task-id>
git task complete --id <task-id> --attempt <attempt-id> --commit <commit-oid> --yes
git task archive --id <task-id>
```

`complete` явно отмечает выбранный подход завершённым с основанием `manual`.
Указанный `--commit` должен входить в целевую ветку. Без `--yes` нужен
интерактивный терминал для подтверждения. Команда не проверяет качество кода.

`archive` убирает задачу из обычного списка, сохраняя историю, код и ветку.
Команды отмены архивирования пока нет.
Незавершённый подход замораживается, его ветка освобождается для `attach`.

### Импорт и экспорт

Чтобы сразу добавить много задач, создайте в своём проекте UTF-8 файл `plan.md`
(можно назвать `PLAN.md`) с плоским Markdown-чеклистом:

```markdown
# План проекта

## Поиск
- [ ] Добавить экран поиска
- [ ] Добавить фильтры

## Подготовка
- [x] Создать репозиторий
```

`- [ ]` означает новую задачу, `- [x]` или `- [X]` — завершённую. Каждый пункт
занимает одну строку без отступа; порядок строк задаёт порядок задач.
Заголовки с `#` и пустые строки разрешены и не становятся задачами.
Вложенные списки, отдельные описания, таблицы, обычные абзацы и блоки кода
в файле импорта не поддерживаются. В сам файл копируйте содержимое примера,
без ограждающих строк с тремя обратными кавычками.

До добавления задач через `add` выполните:

```text
git task init --target main
git task import plan.md --yes
git task status
```

Все пункты будут добавлены за один импорт; ID назначаются автоматически.
`--yes` подтверждает импорт без вопроса; уберите его для предпросмотра
с интерактивным подтверждением. Название файла произвольное и передаётся
в команду явно: CLI не ищет `PLAN.md` автоматически. Импорт требует пустого
плана, а дальнейшие изменения Markdown-файла не синхронизируются с CLI.
Для следующих задач используйте `git task add`.

```text
git task import tasks.md
git task import snapshot.json --format json --yes
git task export
git task export --format markdown --output tasks-copy.md
git task export --format json --output snapshot.json
```

Экспорт без `--output` пишет в терминал. Существующий выходной файл не
перезаписывается. JSON сохраняет задачи и подходы, но не локальную очередь,
наблюдения и настройки подключения; для их сохранения нужен backup рабочего
плана.
Экспорт не выполняет сверку: для актуального снимка сначала вызовите `sync`.
Для JSON-импорта обязательно укажите `--format json`.

### Получение и публикация изменений плана

```text
git task team connect --remote origin
git task team fetch
git task team publish
```

После `connect` изменения задач пытаются автоматически опубликоваться.
`team fetch` получает общий план, `team publish` согласует его с локальной
очередью и отправляет изменения. Обычные `status`, `sync` и hooks работают
локально; сами по себе они не публикуют очередь.

При отказе сети или прав уже сохранённая работа остаётся локально, CLI выводит
предупреждение. После устранения причины выполните `team publish`; повторять
`start` или `complete` ради отправки не нужно. Публикация делает до трёх попыток
за 30 секунд. Автоматического фонового повторителя нет.

Обычный `git fetch` может получать служебную ветку, если это разрешает refspec
remote; `team fetch` получает её явно. Слияние кода на Git-хостинге не вызывает
локальные hooks. Настройка обработки на собственном Git-сервере описана
[ниже](#слияние-на-собственном-git-сервере).

### Hooks, диагностика и восстановление

```text
git task hooks install
git task hooks uninstall
git task doctor
git task doctor --repair recover-start --yes
git task doctor --repair restore-backup --yes
git task doctor --repair unlock --yes
```

`hooks uninstall` снимает интеграцию, сохраняя план и чужие обработчики.
Hooks обрабатывают `post-commit`, `post-merge`, `post-checkout`, `post-rewrite`
и `reference-transaction`, без вопросов и сети. Нужны `sh`, `mktemp`, `cat`, `rm`;
на Windows они входят в Git for Windows. Бинарник должен оставаться по постоянному пути.
`doctor` без repair только читает состояние. Выбирайте восстановление по его
диагностике: `recover-start` завершает прерванную операцию, `restore-backup`
восстанавливает последнюю валидную копию, `unlock` снимает подтверждённо устаревшую
блокировку. Backup может не содержать последних изменений; не удаляйте план,
очередь или журналы для обхода ошибки.

Рабочий CLI требует обычный полный репозиторий с одним рабочим деревом;
bare, shallow, partial clone и дополнительные worktree не поддерживаются.

### Ручное подключение hooks

`_hook <event> [аргументы Git]` — служебная точка входа для hooks, не команда
повседневной работы. Используйте её только при ручном подключении ниже.

Автоматическая установка сохраняет поддерживаемые обычные shell-hooks.
Общий `core.hooksPath`, сторонний менеджер, другой интерпретатор или изменённая
обёртка требуют ручного подключения. CLI сохраняет чужие файлы при отказе.
Для менеджера используйте его механизм расширения; общий каталог меняйте
только с разрешения владельца. Можно оставить интеграцию выключенной и
выполнять `git task sync` самостоятельно.

Для обычного shell-hook после его исходной логики сохраните код выхода
и добавьте вызов с абсолютным путём к установленному бинарнику:

```sh
git_task_original_status=$?
if [ -z "${GIT_TASK_OPERATION:-}" ] && [ -z "${GIT_TASK_HOOK:-}" ]; then
    GIT_TASK_HOOK=1 '/path/to/git-task' _hook post-commit < /dev/null ||
        printf '%s\n' 'git-task: выполните git task sync.' >&2
fi
exit "$git_task_original_status"
```

На Windows используйте прямые слеши и путь к `git-task.exe`. Для `post-merge`
и `post-checkout` передайте настоящее имя события и `"$@"`.
Для `post-rewrite` и `reference-transaction` каждому обработчику нужна копия
исходного stdin. Если менеджер этого не обеспечивает, используйте отдельный
`git task sync`; уже прочитанный stdin повторно передать нельзя.

## Обновление и удаление

Начальная установка описана в [README](README.md#russian). На Linux без графического
интерфейса установите скачанный пакет через `sudo apt install ./<файл>.deb`
или `sudo dnf install ./<файл>.rpm`. Установщики Windows/macOS пока не подписаны.
Для macOS Intel доступны переносимые архивы; текущий CI macOS проверяет ARM64.

Если Windows сообщает `git: 'task' is not a git command`, полностью перезапустите
приложение терминала или IDE. Одна новая вкладка может сохранить старый PATH.
Проверьте установленный файл прямым вызовом в PowerShell:

```powershell
& "$env:LOCALAPPDATA\Programs\GitTask\bin\git-task.exe" version
```

Если версия выводится, установка выполнена. Если файла нет, повторите установку
MSI и дождитесь успешного завершения.

Для обновления скачайте установщик новой версии и запустите его. Путь плагина
сохраняется, повторять `init` в проектах не нужно. Затем откройте новый терминал
и проверьте `git task version` и `git task status`. Проектные планы и очереди
установщик не меняет.

Перед удалением выполните `git task hooks uninstall` в проектах, где включали
hooks. На Windows удалите Git Task через список установленных приложений.
На Debian/Ubuntu используйте `sudo apt remove git-task`, на RPM-дистрибутивах —
`sudo dnf remove git-task`. На macOS удалите `/usr/local/bin/git-task`,
`/etc/paths.d/git-task`, `/usr/local/share/doc/git-task` и снимите квитанцию
командой `sudo pkgutil --forget io.github.toothlonely.git-task`.
Каталоги `.git-task` в ваших проектах сохраняются.

Если ранее использовали старый скрипт установки в отдельный проект, перед
переходом на общий установщик снимите его hooks, выполните
`git config --local --unset alias.task` и
`git config --local --unset git-task.install-path` в этом проекте.
После установки нового плагина hooks можно подключить заново.

## Сборка из исходников

Переносимый архив нужно распаковать в постоянный каталог и добавить его в PATH.
С Go 1.26.0+ можно собрать CLI самостоятельно из клона этого репозитория.
Windows, PowerShell:

```powershell
go build -o bin/git-task.exe ./cmd/git-task
$gitTaskBin = Join-Path $PWD 'bin'
$env:PATH = $gitTaskBin + [IO.Path]::PathSeparator + $env:PATH
```

Linux/macOS:

```sh
go build -o bin/git-task ./cmd/git-task
export PATH="$PWD/bin:$PATH"
```

PATH в этих примерах действует в текущем терминале. Собранный бинарник должен
оставаться по постоянному пути, особенно после подключения hooks.

## Слияние на собственном Git-сервере

Для обычного Git-сервера с доступом к hooks есть
[пример post-receive](scripts/git-task-post-receive.sample). Его подключает
администратор, сохраняя существующий обработчик. Пример не устанавливает
интеграцию с GitHub/GitLab SaaS; этим хостингам нужен отдельный серверный триггер.

Создайте отдельный обычный clone для worker, задайте его локальную Git identity,
выполните `git task init --target <branch>` и `git task team connect --remote <name>`.
Серверному процессу нужны `GIT_TASK_WORKER` — путь этого clone,
`GIT_TASK_BINARY` — абсолютный путь бинарника и `GIT_TASK_TARGET` — целевая ветка
(по умолчанию `main`). Worker должен иметь права получения кода и публикации плана.

После обновления целевой ветки обработчик вызывает:

```text
git task team reconcile --before <old-commit-oid> --after <new-commit-oid>
```

Сохранённое событие или очередь повторяются через `git task team reconcile`
без флагов. Если первая запись не удалась, используйте точную команду с OID
из диагностики сервера. Сохраните ветку подхода до обработки события.
Создание/удаление цели, переписанная история, squash и cherry-pick не дают
автоматического завершения. Обработка синхронная, может задержать push до
30 секунд; отказ публикации плана не отменяет успешный push кода.
