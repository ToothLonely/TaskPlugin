package cli

import (
	"fmt"
	"io"
)

func writeHelp(out io.Writer, topic string) error {
	var text string
	switch topic {
	case "":
		text = `Git Task — личный и общий план задач для Git.

Использование: git task <команда>

Доступные команды:
  help [command]  Показать справку
  version         Показать версию
  init            Создать локальный план
  migrate         Перевести старый план на схему 2
  team            Подключить и согласовать общий план
  add             Добавить задачу
  start           Создать ветку и начать задачу
  attach          Привязать существующую ветку
  edit            Изменить название и описание
  move            Переместить задачу в плане
  pause           Приостановить текущую работу
  resume          Переключиться на ветку и продолжить работу
  archive         Архивировать задачу с сохранением истории
  import          Импортировать Markdown или полный JSON
  export          Экспортировать снимок плана
  sync            Сверить план с локальным Git
  complete        Явно завершить задачу
  status          Сверить и показать план
  show            Показать задачу и историю без сверки
  hooks           Установить или удалить локальные hooks
  doctor          Диагностика и явное восстановление

Также доступны --help и --version без Git-репозитория.
`
	case "help":
		text = "Использование: git task help [command]\nПоказать справку. Флаг --help разрешён до и после имени команды.\n"
	case "doctor":
		text = "Использование: git task doctor [--repair recover-start / restore-backup / unlock] [--yes]\nБез repair только локальное чтение, без сети и сверки. Явный repair показывает снимок и требует подтверждение; --yes не отменяет проверки. Очередь публикации: git task team publish, без повторного start/complete. Restore сохраняет повреждённый оригинал и предупреждает о свежих неопубликованных данных; unlock требует отсутствия PID этого компьютера. Подробности: docs/DOCTOR.md.\n"
	case "migrate":
		text = "Использование: git task migrate\nЯвно перейти со схемы 1 на схему 2; исходник сохраняется в plan.schema-1.json и backup. Незавершённая операция блокирует миграцию.\n"
	case "team":
		text = "Использование: git task team connect --remote <name> / fetch / publish / reconcile --before <oid> --after <oid>\nЯвный обмен через git-task-plan: до подключения выполните init (либо migrate). Fetch получает, publish согласует и публикует очередь; максимум 3 попытки и 30 секунд. Локальная работа при отказе сохраняется. Reconcile используется серверным post-receive: docs/P13-IMPLEMENTATION.md.\n"
	case "version":
		text = "Использование: git task version [--help]\nПоказать версию git-task. Репозиторий и Git не требуются.\n"
	case "init":
		text = "Использование: git task init [--target <branch>]\nСоздать локальный план; целевая ветка по умолчанию main.\n"
	case "add":
		text = "Использование: git task add <title> [--description <text>] [--after <id> / --before <id> / --end]\nДобавить задачу todo.\n"
	case "status":
		text = "Использование: git task status [--json]\nЛокальная сверка без сети; прогресс, все active/paused, последнее завершение и первый todo. JSON schema_version=2: docs/STATUS.md.\n"
	case "show":
		text = "Использование: git task show --id <id>\nПрочитать задачу, description, все подходы с ID, автором, состоянием и историей, включая archived. Без sync и записи; предупреждения сохранённые.\n"
	case "sync":
		text = "Использование: git task sync\nСверить active/paused с локальным Git без сети и вопросов. Недостаточные доказательства выводятся как предупреждения.\n"
	case "hooks":
		text = "Использование: git task hooks install / uninstall\nЛокальная интеграция post-commit, post-merge, post-checkout, post-rewrite и reference-transaction. Общий hooksPath и сторонние менеджеры требуют ручного подключения; см. docs/HOOKS.md. Uninstall сохраняет план и чужие изменения.\n"
	case "complete":
		text = "Использование: git task complete --id <id> [--attempt <id>] [--commit <oid>] [--yes]\nЯвное завершение выбранного подхода с основанием manual; задача done после всех подходов; commit должен входить в target_branch. Подтверждение требует терминал или --yes.\n"
	case "edit":
		text = "Использование: git task edit --id <id> [--title <title>] [--description <text>]\nМеняет только указанные поля, сохраняя ID и историю. Без полевых флагов открывает JSON с title/description в редакторе: GIT_EDITOR, core.editor, VISUAL, EDITOR. Неизменённый документ — без записи; конфликт сохраняет результат по указанному пути. Правила кавычек и запуска: docs/LIFECYCLE.md.\n"
	case "move":
		text = "Использование: git task move --id <id> (--after <id> / --before <id> / --end)\nИзменить порядок без изменения ID и номера. Требуется ровно один способ позиционирования.\n"
	case "pause":
		text = "Использование: git task pause --id <id> [--attempt <id>]\nПриостановить active, сохранив подход и ветку. Повтор для paused — без записи.\n"
	case "resume":
		text = "Использование: git task resume --id <id> [--attempt <id>]\nПродолжить paused на связанной существующей ветке; требуется чистое рабочее дерево и обычный HEAD. При потере связи используйте attach <branch> --id <id> --rebind.\n"
	case "archive":
		text = "Использование: git task archive --id <id>\nАрхивировать задачу; код, ветка и история сохраняются. Незавершённый подход замораживается, ветка освобождается для attach. Повтор — без записи; unarchive отсутствует.\n"
	case "start":
		text = "Использование: git task start <branch> [--id <id> / --title <title> / --new <title> / --select] [--from <ref>] [--again]\nБез селектора выбирается первый todo; основание — target_branch. --again требует явного выбора done. --select открывает меню в терминале: номер задачи, n — новая, 0 — отмена.\n"
	case "attach":
		text = "Использование: git task attach <branch> --id <id> [--rebind [--attempt <id>]]\nПривязать существующую локальную ветку как новый подход к todo/active/paused без checkout. --rebind меняет связь active/paused, сохраняя ID подхода и историю связей; чужая занятая ветка запрещена. Повтор подтверждённой связи — без записи. Для done используйте start --again.\n"
	case "import":
		text = "Использование: git task import <path> [--format markdown / --format json] [--yes]\nПредпросмотр и импорт в пустой инициализированный план. По умолчанию markdown. Подтверждение требует терминал или --yes. Источник не изменяется.\n"
	case "export":
		text = "Использование: git task export [--format markdown / --format json] [--output <path>]\nСнимок без sync; по умолчанию markdown в stdout. Существующий output не перезаписывается. JSON сохраняет общий снимок без локальных наблюдений, очереди и настроек подключения.\n"
	default:
		if planned(topic) {
			return fmt.Errorf("команда %q ещё не реализована", topic)
		}
		return &usageError{fmt.Sprintf("нет справки для неизвестной команды %q", topic)}
	}
	_, err := io.WriteString(out, text)
	return err
}
