# Экран возвращения и JSON status

> Уточнение 2026-10-04: ниже описана принятая реализация P01–P12 со схемой 1. Целевой контракт P13 с несколькими attempts, author, общей веткой git-task-plan и offline-очередью находится в [PLAN.md](../PLAN.md#контракт-start-и-attempts) и [SPEC §11](SPEC.md#11-целевой-контракт-p13-общий-план-и-параллельные-подходы). Исполнитель P13 обновляет этот документ по фактической реализации; наличие нового требования не означает, что оно уже работает.

`status` выполняет общую локальную сверку sync/tracking. Сеть и диалоги не используются. История ограничена существующим бюджетом tracking: до 4096 записей/коммитов на проверяемую историю, reflog до 4 MiB. Стоимость растёт с количеством задач и подходов; общего ограничения времени всей команды нет. Незавершённая операция хранения даёт последний согласованный план и предупреждение без repair. Неизменившаяся сверка не сохраняет файл.

Порядок экрана: цель; текущая ветка и связанная active/paused задача либо отсутствие связи; прогресс; все active/paused в порядке плана с ветками продолжения; последнее зарегистрированное завершение; первый todo; полный список плана; предупреждения с действиями. Detached HEAD и ветка без первого коммита подписываются отдельно. Архивная замороженная связь не является текущей задачей.

Прогресс: текущие done / все не archived. При total=0 процент не определён. Последнее завершение выбирается по максимальному completion.event среди всей истории, включая повторно начатые и archived задачи. Текущий статус этой задачи показывается рядом с историческим событием. Следующая задача выбирается общим Plan.FirstTodo, как start без селектора.

`show --id <id>` читает сохранённую задачу, включая archived, без sync и записи. Description хранит необязательный краткий контекст; команды note нет. Незавершённый подход, замороженный подход archived и завершённая история подписаны отдельно. Отображаются исходная/текущая ветка, цель, основание, наблюдённые commit ID, перепривязки и completion. Даты начала, регистрации наблюдения и известного завершения различаются. Отсутствующая дата не вычисляется по другим датам. Status показывает время Git-коммита HEAD (committer date) как известное событие Git, с прямой оговоркой, что это не время последней работы человека. Show выводит даты модели без дополнительных Git-запросов. Управляющие символы в текстовых названиях/описаниях/сообщениях заменяются пробелами; JSON сохраняет исходный текст с экранированием JSON.

## Машинный контракт v1

`status --json` пишет ровно один объект и перевод строки в stdout. Предупреждения также выводятся в stderr. Ошибка до готового снимка оставляет stdout пустым; ошибка самого writer может дать неполный поток. ANSI и вывод процессов Git не добавляются. Порядок массивов стабилен по порядку плана. Дополнительные необязательные поля допустимы; изменение смысла или типа существующих требует новой schema_version.

| Поле | Тип и смысл |
|---|---|
| schema_version | integer, 1; версия представления, отдельно от файла хранения |
| repository | object: root (string, абсолютный путь), target_branch (string), current_branch (string/null), head_commit (string/null), head_committed_at (RFC3339 string/null, committer date HEAD), detached (boolean), unborn (boolean) |
| progress | object: done (integer), total (integer), percent (number/null), округление 100*done/total до одного десятичного знака |
| tasks | array всех Task в полном порядке, включая archived; пустой массив [] |
| current_task_id | string/null; только active/paused с веткой HEAD |
| last_completion | object/null: task_id (string), task_status (string), attempt_id (string), completion (полный Completion) |
| next_task_id | string/null; первый todo |
| warnings | array объектов code (string), message (string), task_id (необязательный string); пустой массив [] |

Предупреждения задач сохраняют коды tracking. `operation_pending` обозначает незавершённый журнал и необходимость явного восстановления. Ветка/commit отсутствуют как null, когда они неизвестны, а не как придуманное значение. Tasks сохраняет правило отсутствия attempts до первого завершения и разделение active_attempt/attempts. Show не имеет JSON-режима в v1.

Вложенные объекты имеют следующую схему. Все поля обязательны, кроме явно перечисленных необязательных. Необязательные пустые строки, массивы и объекты отсутствуют, а не сериализуются как null. Время — строка RFC3339 с необязательными долями секунды; неизвестные даты внутри модели отсутствуют. Revision и event — беззнаковые 64-битные JSON integers; потребитель должен сохранять точность, если значения превышают безопасный диапазон его числового типа. Инварианты и допустимые комбинации: [MODEL.md](MODEL.md).

| Объект | Поля |
|---|---|
| Task | id, number, title (string); revision (integer); status (string: todo/active/paused/done/archived); необязательные description (string), active_attempt (Attempt), attempts (array Attempt), warnings (array Warning) |
| Attempt | id (string); необязательные branch, original_branch, target_branch, base_commit (string), started_at (время), rebindings (array Rebinding), completion (Completion), observation (Observation) |
| Completion | event (integer), source (string: merge/manual/imported), target_branch (string); необязательные completed_at, observed_at (время), commit, work_commit (string), merge_kind (string: merge_commit/fast_forward) |
| Observation | tip, target_commit (string); необязательные work_commit, branch_log, target_log (string) |
| Rebinding | from, to, base_commit (string), observed_at (время) |
| Warning внутри Task | code, message (string); task_id есть только в верхнем массиве warnings |

Пустой инициализированный проект с unborn main:

```json
{"schema_version":1,"repository":{"root":"/project","target_branch":"main","current_branch":"main","head_commit":null,"head_committed_at":null,"detached":false,"unborn":true},"progress":{"done":0,"total":0,"percent":null},"tasks":[],"current_task_id":null,"last_completion":null,"next_task_id":null,"warnings":[]}
```

Обычный экран показывает, например, `Прогресс: 1 из 3 (33.3%)`, `active ... ветка: feature/login; цель: main`, `paused ... ветка: feature/profile; цель: main`, затем последнее событие и первый todo. При пропавшей ветке active сохраняется и выводится `branch_missing` с предложением явного attach --rebind. При повторном start прежнее завершение видно с текущим статусом active, но задача исключена из числителя прогресса.

Примеры ниже описывают ожидаемый вывод; они не являются отчётом о запущенных сценариях.

Пустой план без первого коммита:

```text
Целевая ветка: main
Текущая ветка: main (ещё нет Git-коммита)
Текущая задача: нет связи с active/paused.
Прогресс: 0 из 0, процент не определён
Активные и приостановленные задачи:
  нет.
Последнее зарегистрированное завершение: нет.
Следующая задача (первый todo): нет.
План:
  План пуст.
Предупреждения:
  нет.
```

Фрагмент обычного проекта с несколькими задачами:

```text
Прогресс: 1 из 4 (25.0%)
Активные и приостановленные задачи:
  T-001 [task-001] active — Вход; ветка: feature/login; цель: main
    Контекст: Осталось проверить ввод пароля
  T-002 [task-002] paused — Профиль; ветка: feature/profile; цель: main
Последнее зарегистрированное завершение: task-004, подход manual-1; текущий статус done; источник manual; событие 1
```

Фрагмент проблемного проекта с удалённой веткой:

```text
Активные и приостановленные задачи:
  T-001 [task-001] active — Вход; ветка: feature/login; цель: main
Предупреждения:
  [branch_missing] task-001: Ветка подхода отсутствует; используйте явный attach --rebind.
```

Завершение не выдумывается по отсутствию ветки. Show для этого же ID оставляет незавершённый подход отдельно от истории и подписывает предупреждение как сохранённое.

Проверяющий запускает базовые Go-проверки и сценарии status/show из internal/app/status_test.go и internal/cli/status_test.go. Существующие sync-тесты проверяют обычный merge/fast-forward, пропущенные hooks, пустую ветку, squash и неполные доказательства. Дополнительно следует проверить CLI в изолированном репозитории с несколькими active, paused, done, archived и потерянной веткой; распарсить JSON; сравнить plan.json, backup, refs и код после повторного status; проверить show до sync после изменения Git, detached и unborn, ошибки вывода и повреждённый план.
