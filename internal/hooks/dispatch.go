package hooks

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func Validate(event string, args []string, input io.Reader) error {
	switch event {
	case "post-commit":
		if len(args) != 0 {
			return fmt.Errorf("post-commit не принимает аргументы")
		}
	case "post-merge":
		if len(args) != 1 || args[0] != "0" && args[0] != "1" {
			return fmt.Errorf("post-merge требует флаг 0 или 1")
		}
	case "post-checkout":
		if len(args) != 3 || !oid(args[0]) || !oid(args[1]) || len(args[0]) != len(args[1]) || args[2] != "0" && args[2] != "1" {
			return fmt.Errorf("post-checkout требует old oid, new oid и флаг 0 или 1")
		}
	case "post-rewrite":
		if len(args) < 1 || args[0] != "amend" && args[0] != "rebase" {
			return fmt.Errorf("post-rewrite требует amend или rebase")
		}
		if input == nil {
			return fmt.Errorf("post-rewrite требует поток stdin")
		}
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			old, rest, ok := strings.Cut(scanner.Text(), " ")
			new, _, _ := strings.Cut(rest, " ")
			if !ok || !oid(old) || !oid(new) || len(old) != len(new) {
				return fmt.Errorf("неверная пара old/new oid в stdin post-rewrite")
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("чтение stdin post-rewrite: %w", err)
		}
	case "reference-transaction":
		if len(args) != 1 || args[0] != "prepared" && args[0] != "committed" && args[0] != "aborted" {
			return fmt.Errorf("неверная фаза reference-transaction")
		}
		if input == nil {
			return fmt.Errorf("reference-transaction требует stdin")
		}
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			parts := strings.Split(scanner.Text(), " ")
			if len(parts) != 3 || !oid(parts[0]) || !oid(parts[1]) || len(parts[0]) != len(parts[1]) || parts[2] == "" {
				return fmt.Errorf("неверная запись reference-transaction")
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("неизвестное событие hook %q", event)
	}
	return nil
}

func oid(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
