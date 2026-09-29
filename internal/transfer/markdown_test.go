package transfer

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"git-task/internal/task"
)

func TestMarkdownOrderUnicodeAndImportedHistory(t *testing.T) {
	input := "\ufeff# План\r\n\r\n- [ ]  Одинаковая 🙂 \r\n- [x] Готово 日本語\r\n## Этап\r\n- [ ]  Одинаковая 🙂 \r\n- [X] Последняя\r\n- [ ] Хвост\r\n"
	plan, err := DecodeMarkdown([]byte(input), "main")
	if err != nil {
		t.Fatal(err)
	}
	wantTitles := []string{" Одинаковая 🙂 ", "Готово 日本語", " Одинаковая 🙂 ", "Последняя", "Хвост"}
	for i, id := range plan.Order {
		item, err := plan.FindID(id)
		if err != nil || item.Title != wantTitles[i] {
			t.Fatalf("item %d: %+v %v", i, item, err)
		}
		if i == 1 || i == 3 {
			if item.Status != task.Done || len(item.Attempts) != 1 || item.ActiveAttempt != nil {
				t.Fatalf("done: %+v", item)
			}
			a := item.Attempts[0]
			if a.Completion.Source != task.Imported || a.StartedAt != nil || a.Branch != "" || a.BaseCommit != "" || a.Completion.CompletedAt != nil || a.Completion.ObservedAt != nil || a.Completion.Commit != "" {
				t.Fatalf("invented evidence: %+v", a)
			}
		} else if item.Status != task.Todo || len(item.Attempts) != 0 || item.ActiveAttempt != nil {
			t.Fatalf("todo: %+v", item)
		}
	}
	if plan.LastEvent != 2 || plan.InsertionTail != "task-004" {
		t.Fatalf("chronology: %+v", plan)
	}
	if _, err = plan.Add("После последней готовой", "", task.Position{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Order, []string{"task-001", "task-002", "task-003", "task-004", "task-006", "task-005"}) {
		t.Fatalf("insertion: %v", plan.Order)
	}
}

func TestMarkdownTodoTailAndLongLine(t *testing.T) {
	title := strings.Repeat("я", 70000)
	plan, err := DecodeMarkdown([]byte("- [ ] Первый\n- [ ] "+title), "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Add("Третий", "", task.Position{}); err != nil || plan.Order[2] != "task-003" {
		t.Fatalf("tail: %v %v", plan.Order, err)
	}
	data, err := EncodeMarkdown(plan)
	if err != nil || !bytes.Contains(data, []byte(title)) {
		t.Fatalf("long title lost: %v", err)
	}
}

func TestMarkdownRejectsUnsupportedInputWithoutPartialPlan(t *testing.T) {
	for name, line := range map[string]string{
		"nested": "  - [ ] Вложенный", "tab": "\t- [x] Вложенный",
		"prose": "Описание", "continuation": "  продолжение", "code": "```",
		"marker": "* [ ] Другой", "numbered": "1. [ ] Другой", "no separator": "- [x]title",
		"bad check": "- [v] Другой", "empty": "- [ ] ", "blank": "- [ ] \t ",
		"bare CR": "- [ ] X\rY", "invalid UTF8": "- [ ] \xff", "heading": "#нет пробела",
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := DecodeMarkdown([]byte("- [ ] Первый\n"+line+"\n- [ ] Последний\n"), "main")
			if err == nil || !strings.Contains(err.Error(), "строка 2") || len(plan.Tasks) != 0 {
				t.Fatalf("partial=%+v err=%v", plan, err)
			}
		})
	}
	for _, input := range []string{"", "\ufeff# План\n\t\n", "- [ ] X\r"} {
		if _, err := DecodeMarkdown([]byte(input), "main"); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestMarkdownExportUsesOrderAndCurrentStatus(t *testing.T) {
	plan, err := DecodeMarkdown([]byte("- [ ] Первый\n- [x] Готовый\n- [ ] Архив"), "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Archive("task-003"); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Move("task-003", task.Position{Before: "task-001"}); err != nil {
		t.Fatal(err)
	}
	data, err := EncodeMarkdown(plan)
	if err != nil || string(data) != "- [ ] Архив\n- [ ] Первый\n- [x] Готовый\n" {
		t.Fatalf("export %q %v", data, err)
	}
	for _, title := range []string{"Две\nстроки", "Две\rстроки", "Две\u2028строки"} {
		if _, err = plan.Edit("task-001", task.EditOptions{Title: &title}); err != nil {
			t.Fatal(err)
		}
		if data, err = EncodeMarkdown(plan); err == nil || data != nil || !strings.Contains(err.Error(), "json") {
			t.Fatalf("multiline: %q %v", data, err)
		}
	}
}

func FuzzMarkdown(f *testing.F) {
	for _, input := range []string{"- [ ] Задача\n- [X] 完了", "\ufeff# План\r\n- [x] X\r\n", "- [ ] OK\n  - [ ] nested", "- [", ""} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		plan, err := DecodeMarkdown(data, "main")
		if err != nil {
			if len(plan.Tasks) != 0 {
				t.Fatal("partial plan on error")
			}
			return
		}
		if err = plan.Validate(); err != nil {
			t.Fatal(err)
		}
		encoded, err := EncodeMarkdown(plan)
		if err != nil {
			return
		}
		again, err := DecodeMarkdown(encoded, "main")
		if err != nil || !reflect.DeepEqual(again, plan) {
			t.Fatalf("round-trip: %v", err)
		}
	})
}

func TestMarkdownExportForEveryStatus(t *testing.T) {
	plan, err := DecodeMarkdown([]byte("- [x] Done\n- [ ] Todo\n- [ ] Active\n- [ ] Paused\n- [ ] Archived\n- [x] Again"), "main")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"task-003", "task-004", "task-006"} {
		branch := "branch-" + id
		if _, err = plan.Start(id, task.Attempt{ID: "active-" + id, Branch: branch, OriginalBranch: branch, TargetBranch: "main", BaseCommit: strings.Repeat("a", 40), StartedAt: &now}, id == "task-006"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = plan.Pause("task-004"); err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Archive("task-005"); err != nil {
		t.Fatal(err)
	}
	data, err := EncodeMarkdown(plan)
	if err != nil || string(data) != "- [x] Done\n- [ ] Todo\n- [ ] Active\n- [ ] Paused\n- [ ] Archived\n- [ ] Again\n" {
		t.Fatalf("status mapping: %q %v", data, err)
	}
}
