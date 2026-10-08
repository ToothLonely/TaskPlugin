# Git Task

[English](#english) | [Русский](#russian)

<a id="english"></a>

Git Task extends Git with task tracking: the `git task` command links a task
to a branch and updates its status as work progresses.

## 1. Install Git Task

You need Git 2.51.0 or later. Open
[Releases](https://github.com/ToothLonely/TaskPlugin/releases), download the installer
for your system, and run it:

| System | Installer |
|---|---|
| Windows x64, Intel/AMD | `.msi` with `windows` in its name |
| macOS, Apple Silicon | `.pkg` with `darwin_arm64` in its name |
| Debian/Ubuntu x64, Intel/AMD | `.deb` with `linux` in its name |
| Fedora/RHEL x64, Intel/AMD | `.rpm` with `linux` in its name |

After installation, fully restart your terminal application or IDE and check:

```text
git task version
```

## 2. Set up your project and fill in plan.json

Go to the root of your Git project. It must have at least one commit,
a configured Git identity (`user.name` and `user.email`), and a branch to merge
your work into. This example uses `main`.

Run:

```text
git task init
```

The command creates the `.git-task` directory and a `plan.json` file excluded from Git:

```json
{
  "target_branch": "",
  "tasks": []
}
```

Open `.git-task/plan.json`, set the target branch, and list tasks in the desired order.
The required fields are `target_branch`, the `tasks` array, and a nonempty `title`
for each task. Save the file as UTF-8. Here is a filled-in example:

```json
{
  "target_branch": "main",
  "tasks": [
    { "title": "Add a search screen" },
    { "title": "Add filters" }
  ]
}
```

Save the file and explicitly finish setup:

```text
git task init --apply
git task hooks install
git task status
```

`--apply` generates task IDs, `order` from the sequence in `tasks`, initial statuses,
and internal fields. You do not need to enter them manually. Applying a ready plan
again preserves it and its history. If your target is `master`, replace `main`
in the JSON and commands. After setup, add more tasks with `git task add "Title"`.

## 3. Start a task

Your working tree must be clean: save code changes in a regular commit.
Then specify a name for the new branch:

```text
git task start feature/search
```

Git Task selects the first `todo` task, creates a branch from `main`, switches to it,
and marks the task as `active`. The task gains an `attempts` array with a record
of this attempt: its ID, author, branch, and starting point. In this example,
the task is “Add a search screen”.
To choose a different task instead, provide its exact title:

```text
git task start feature/filters --title "Add filters"
```

You can also select a task by a unique prefix of its ID from `git task status`.
For example, if only one ID starts with `9f86`:

```text
git task start feature/search --id 9f86
git task show --id 9f86
```

If the prefix matches multiple tasks, the command lists their IDs and asks you
to provide more characters. Full IDs still work.

## 4. How automatic tracking works

Write code, make commits, and merge your branch with regular Git commands:

```text
git add .
git commit -m "Add a search screen"
git switch main
git merge feature/search
git task status
```

Installed hooks update the plan after Git operations. When the current attempt's
commits reach the target branch through a regular merge or fast-forward, the attempt
is completed. The task becomes `done` when all its known attempts are complete.
The next `git task start <new-branch>` picks the next `todo` task.

This “bot” tracks state locally: it does not write code, make commits, or merge
branches for you. Without hooks, `git task status` reconciles the plan with Git.
An empty or deleted branch does not mean completion; squash and cherry-pick require
an explicit `git task complete`. Merging on GitHub/GitLab does not itself run
local hooks.

## Working in a team

You need a shared Git repository with an `origin` remote and permission to push.
One team member fills in and applies the plan as shown above, then publishes it:

```text
git task team connect --remote origin
```

Other team members create an empty plan in their own copies of the project and
connect to the shared plan. Use the same target branch as the first team member:

```text
git task init --target main
git task hooks install
git task team connect --remote origin
```

Connecting downloads the existing tasks and their IDs. The shared plan lives
in a separate `git-task-plan` branch in the same repository; push code with regular Git.
Before choosing a task, run `git task team fetch`, then use `git task start`
as shown above. Task changes are automatically submitted for publication.
After merging code, run `git task team publish` to send status changes that hooks
saved locally. If the network is unavailable, your work stays in a local queue;
run `team publish` again once connectivity is restored.

[All commands and additional settings → DOCUMENTATION.md](https://github.com/ToothLonely/TaskPlugin/blob/master/DOCUMENTATION.md#english)

---

<a id="russian"></a>

# Git Task

Git Task — дополнение к Git для ведения задач: команда `git task` связывает задачу
с веткой и обновляет её статус по результатам работы.

## 1. Установите Git Task

Нужен Git 2.51.0 или новее. Откройте
[Releases](https://github.com/ToothLonely/TaskPlugin/releases), скачайте установщик
для своей системы и запустите его:

| Система | Установщик |
|---|---|
| Windows x64, Intel/AMD | `.msi` с `windows` в имени |
| macOS, Apple Silicon | `.pkg` с `darwin_arm64` в имени |
| Debian/Ubuntu x64, Intel/AMD | `.deb` с `linux` в имени |
| Fedora/RHEL x64, Intel/AMD | `.rpm` с `linux` в имени |

После установки полностью перезапустите приложение терминала или IDE и проверьте:

```text
git task version
```

## 2. Подключите проект и заполните plan.json

Перейдите в корень своего Git-проекта. В нём должны быть хотя бы один коммит,
настроенная Git identity (`user.name` и `user.email`) и ветка, в которую вы будете
вливать работу. В примере это `main`.

Выполните:

```text
git task init
```

Команда создаст каталог `.git-task` и файл `plan.json`, исключённый из Git:

```json
{
  "target_branch": "",
  "tasks": []
}
```

Откройте `.git-task/plan.json`, укажите целевую ветку и задачи в нужном порядке.
Обязательны `target_branch`, массив `tasks` и непустой `title` у каждой задачи.
Файл должен быть в UTF-8. Пример заполнения:

```json
{
  "target_branch": "main",
  "tasks": [
    { "title": "Добавить экран поиска" },
    { "title": "Добавить фильтры" }
  ]
}
```

Сохраните файл и явно завершите настройку:

```text
git task init --apply
git task hooks install
git task status
```

`--apply` создаст ID задач, `order` по порядку массива `tasks`, начальные статусы
и служебные поля. Вручную их вводить не нужно. Повторное применение сохраняет
готовый план и историю. Если цель — `master`, замените `main` в JSON и командах.
После настройки новые задачи можно добавлять через `git task add "Название"`.

## 3. Начните задачу

Рабочее дерево должно быть чистым: сохраните изменения кода обычным коммитом.
Затем укажите имя новой ветки:

```text
git task start feature/search
```

Git Task выберет первую задачу `todo`, создаст ветку от `main`, переключится на неё
и отметит задачу как `active`. У задачи появится массив `attempts` с записью
этого подхода: его ID, автором, веткой и основанием. В примере начинается
«Добавить экран поиска».
Для выбора другой задачи вместо этого укажите её точное название:

```text
git task start feature/filters --title "Добавить фильтры"
```

Также можно выбрать задачу по уникальному началу ID из `git task status`.
Например, если с `9f86` начинается только один ID:

```text
git task start feature/search --id 9f86
git task show --id 9f86
```

Если префикс совпал с несколькими задачами, команда покажет их ID и попросит
ввести больше символов. Полный ID продолжает работать.

## 4. Как работает автоматическое отслеживание

Вы пишете код, делаете коммиты и сливаете ветку обычными командами Git:

```text
git add .
git commit -m "Добавить экран поиска"
git switch main
git merge feature/search
git task status
```

Подключённые hooks обновляют план после Git-операций. Когда коммиты текущего
подхода попадут в целевую ветку через обычный merge или fast-forward, подход
будет завершён. Задача станет `done`, когда завершены все её известные подходы.
Следующий `git task start <новая-ветка>` возьмёт следующую задачу `todo`.

Этот «бот» отслеживает состояние локально: он не пишет код, не делает коммиты
и не сливает ветки за вас. Без hooks сверка выполняется при `git task status`.
Пустая или удалённая ветка не означает завершение; squash и cherry-pick требуют
явного `git task complete`. Слияние на GitHub/GitLab само по себе не запускает
локальные hooks.

## Для работы в группе

Нужен общий Git-репозиторий с remote `origin` и правом отправлять изменения.
Один участник заполняет и применяет план по примеру выше, затем публикует его:

```text
git task team connect --remote origin
```

Остальные участники в своих копиях проекта создают пустой план и подключаются
к общему. Укажите ту же целевую ветку, которую выбрал первый участник:

```text
git task init --target main
git task hooks install
git task team connect --remote origin
```

При подключении они получат готовые задачи и их ID. Общий план хранится
в отдельной ветке `git-task-plan` того же репозитория; код отправляйте обычным Git.
Перед выбором задачи выполните `git task team fetch`, затем работайте через
`git task start`, как показано выше. Изменения задач пытаются публиковаться
автоматически. После слияния кода выполните `git task team publish`, чтобы
отправить изменения статусов, которые hooks сохранили локально. Если сеть
недоступна, работа останется в локальной очереди; повторите `team publish`
после восстановления связи.

[Все команды и дополнительные настройки → DOCUMENTATION.md](https://github.com/ToothLonely/TaskPlugin/blob/master/DOCUMENTATION.md#russian)
