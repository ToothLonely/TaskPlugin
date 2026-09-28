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
	default:
		if planned(topic) {
			return fmt.Errorf("команда %q ещё не реализована", topic)
		}
		return &usageError{fmt.Sprintf("нет справки для неизвестной команды %q", topic)}
	}
	_, err := io.WriteString(out, text)
	return err
}
