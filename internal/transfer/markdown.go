package transfer

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"git-task/internal/task"
)

func DecodeMarkdown(data []byte, target string) (task.Plan, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	plan, err := task.NewPlan(target)
	if err != nil {
		return task.Plan{}, err
	}
	var completed []string
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i < len(lines)-1 {
			line = strings.TrimSuffix(line, "\r")
		}
		if !utf8.ValidString(line) || strings.ContainsRune(line, '\r') {
			return task.Plan{}, fmt.Errorf("строка %d: требуется UTF-8 и LF/CRLF", i+1)
		}
		if strings.TrimSpace(line) == "" || heading(line) {
			continue
		}
		if len(line) < 6 || line[:3] != "- [" || line[4:6] != "] " || !strings.ContainsRune(" xX", rune(line[3])) {
			return task.Plan{}, fmt.Errorf("строка %d: ожидается плоский пункт - [ ] title или - [x] title", i+1)
		}
		added, err := plan.Add(line[6:], "", task.Position{})
		if err != nil {
			return task.Plan{}, fmt.Errorf("строка %d: %w", i+1, err)
		}
		if line[3] != ' ' {
			completed = append(completed, added.ID)
		}
	}
	if len(plan.Tasks) == 0 {
		return task.Plan{}, fmt.Errorf("Markdown не содержит пунктов чеклиста")
	}
	for _, id := range completed {
		if _, err := plan.Complete(id, "imported-"+id, task.Completion{Source: task.Imported, TargetBranch: target}); err != nil {
			return task.Plan{}, err
		}
	}
	return plan, nil
}

func heading(line string) bool {
	count := len(line) - len(strings.TrimLeft(line, "#"))
	return count >= 1 && count <= 6 && (count == len(line) || line[count] == ' ' || line[count] == '\t')
}

func EncodeMarkdown(plan task.Plan) ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	var out strings.Builder
	for _, id := range plan.Order {
		item, err := plan.FindID(id)
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(item.Title, "\r\n\u0085\u2028\u2029") {
			return nil, fmt.Errorf("задача %q: title содержит перевод строки; используйте --format json", id)
		}
		mark := " "
		if item.Status == task.Done {
			mark = "x"
		}
		fmt.Fprintf(&out, "- [%s] %s\n", mark, item.Title)
	}
	return []byte(out.String()), nil
}
