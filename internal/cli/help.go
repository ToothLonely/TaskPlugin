package cli

import (
	"fmt"
	"io"
)

func writeHelp(out io.Writer, topic string) error {
	var text string
	switch topic {
	case "":
		text = `Git Task — локальный план задач для Git.

Использование: git task <команда>

Доступные команды:
  help [command]  Показать справку
  version         Показать версию
  init            Создать локальный план
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
  hooks           Установить или удалить локальные hooks

Также доступны --help и --version без Git-репозитория.
`
	case "help":
		text = "Использование: git task help [command]\nПоказать справку. Флаг --help разрешён до и после имени команды.\n"
	case "version":
		text = "Использование: git task version [--help]\nПоказать версию git-task. Репозиторий и Git не требуются.\n"
	case "init":
		text = "Использование: git task init [--target <branch>]\nСоздать локальный план; целевая ветка по умолчанию main.\n"
	case "add":
		text = "Использование: git task add <title> [--description <text>] [--after <id> / --before <id> / --end]\nДобавить задачу todo.\n"
	case "status":
		text = "Использование: git task status\nСверить и показать план. --json пока недоступен.\n"
	case "sync":
		text = "Использование: git task sync\nСверить active/paused с локальным Git без сети и вопросов. Недостаточные доказательства выводятся как предупреждения.\n"
	case "hooks":
		text = "Использование: git task hooks install / uninstall\nЛокальная интеграция post-commit, post-merge, post-checkout и post-rewrite. Общий hooksPath и сторонние менеджеры требуют ручного подключения; см. docs/HOOKS.md. Uninstall сохраняет план и чужие изменения.\n"
	case "complete":
		text = "Использование: git task complete --id <id> [--commit <oid>] [--yes]\nЯвное завершение с основанием manual; commit должен входить в target_branch. Подтверждение требует терминал или --yes.\n"
	case "edit":
		text = "Использование: git task edit --id <id> [--title <title>] [--description <text>]\nМеняет только указанные поля, сохраняя ID и историю. Без полевых флагов открывает JSON с title/description в редакторе: GIT_EDITOR, core.editor, VISUAL, EDITOR. Неизменённый документ — без записи; конфликт сохраняет результат по указанному пути. Правила кавычек и запуска: docs/LIFECYCLE.md.\n"
	case "move":
		text = "Использование: git task move --id <id> (--after <id> / --before <id> / --end)\nИзменить порядок без изменения ID и номера. Требуется ровно один способ позиционирования.\n"
	case "pause":
		text = "Использование: git task pause --id <id>\nПриостановить active, сохранив подход и ветку. Повтор для paused — без записи.\n"
	case "resume":
		text = "Использование: git task resume --id <id>\nПродолжить paused на связанной существующей ветке; требуется чистое рабочее дерево и обычный HEAD. При потере связи используйте attach <branch> --id <id> --rebind.\n"
	case "archive":
		text = "Использование: git task archive --id <id>\nАрхивировать задачу; код, ветка и история сохраняются. Незавершённый подход замораживается, ветка освобождается для attach. Повтор — без записи; unarchive отсутствует.\n"
	case "start":
		text = "Использование: git task start <branch> [--id <id> / --title <title> / --new <title> / --select] [--from <ref>] [--again]\nБез селектора выбирается первый todo; основание — target_branch. --again требует явного выбора done. --select открывает меню в терминале: номер задачи, n — новая, 0 — отмена.\n"
	case "attach":
		text = "Использование: git task attach <branch> --id <id> [--rebind]\nПривязать существующую локальную ветку к todo без checkout. --rebind меняет связь active/paused, сохраняя ID подхода и историю связей; чужая занятая ветка запрещена. Повтор подтверждённой связи — без записи. Для done используйте start --again.\n"
	case "import":
		text = "Использование: git task import <path> [--format markdown / --format json] [--yes]\nПредпросмотр и импорт в пустой инициализированный план. По умолчанию markdown. Подтверждение требует терминал или --yes. Источник не изменяется.\n"
	case "export":
		text = "Использование: git task export [--format markdown / --format json] [--output <path>]\nСнимок без sync; по умолчанию markdown в stdout. Существующий output не перезаписывается. JSON сохраняет полное состояние.\n"
	default:
		if planned(topic) {
			return fmt.Errorf("команда %q ещё не реализована", topic)
		}
		return &usageError{fmt.Sprintf("нет справки для неизвестной команды %q", topic)}
	}
	_, err := io.WriteString(out, text)
	return err
}
