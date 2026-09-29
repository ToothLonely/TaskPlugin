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
  import          Импортировать Markdown или полный JSON
  export          Экспортировать снимок плана
  status          Показать сохранённый план

Также доступны --help и --version без Git-репозитория.
Status пока показывает сохранённые данные без сверки с Git.
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
		text = "Использование: git task status\nПоказать сохранённый план. Сверка с Git и --json пока недоступны.\n"
	case "start":
		text = "Использование: git task start <branch> [--id <id> / --title <title> / --new <title> / --select] [--from <ref>] [--again]\nБез селектора выбирается первый todo; основание — target_branch. --again требует явного выбора done. --select открывает меню в терминале: номер задачи, n — новая, 0 — отмена.\n"
	case "attach":
		text = "Использование: git task attach <branch> --id <id> [--rebind]\nПривязать существующую локальную ветку без checkout. --rebind меняет связь active/paused.\n"
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
