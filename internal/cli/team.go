package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

func runTeam(ctx context.Context, command string, args []string, streams Streams, open OpenPlans) error {
	if command == "migrate" {
		positionals, help, err := parseInfoArgs(command, args)
		if err != nil {
			return err
		}
		if len(positionals) != 0 {
			return &usageError{"migrate не принимает аргументы"}
		}
		if help {
			return writeHelp(streams.Out, command)
		}
		if open == nil {
			return fmt.Errorf("операции плана не подключены")
		}
		p, err := open(ctx)
		if err != nil {
			return err
		}
		changed, err := p.Migrate(ctx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(streams.Out, "Схема 2; миграция выполнена: %t.\n", changed)
		return err
	}
	if len(args) == 0 || len(args) == 1 && args[0] == "--help" {
		return writeHelp(streams.Out, "team")
	}
	verb := args[0]
	if verb != "connect" && verb != "fetch" && verb != "publish" && verb != "reconcile" {
		return &usageError{"team требует connect, fetch, publish или reconcile"}
	}
	fs := flag.NewFlagSet("team "+verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var remote string
	var before, after string
	var help bool
	fs.BoolVar(&help, "help", false, "")
	if verb == "connect" {
		fs.StringVar(&remote, "remote", "", "")
	}
	if verb == "reconcile" {
		fs.StringVar(&before, "before", "", "")
		fs.StringVar(&after, "after", "", "")
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		name, value, has := strings.Cut(args[i], "=")
		if seen[name] {
			return &usageError{"повтор флага " + name}
		}
		seen[name] = true
		if name == "--help" {
			if has {
				return &usageError{"--help не принимает значение"}
			}
		} else if name == "--remote" && verb == "connect" || verb == "reconcile" && (name == "--before" || name == "--after") {
			if !has {
				i++
				if i == len(args) {
					return &usageError{"--remote требует имя"}
				}
				value = args[i]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return &usageError{"неверное имя remote"}
			}
			name += "=" + value
		} else {
			return &usageError{"неверный аргумент team: " + args[i]}
		}
		if err := fs.Parse([]string{name}); err != nil {
			return &usageError{err.Error()}
		}
	}
	if help {
		return writeHelp(streams.Out, "team")
	}
	if verb == "connect" && remote == "" {
		return &usageError{"team connect требует --remote <name>"}
	}
	if verb == "reconcile" && (before == "") != (after == "") {
		return &usageError{"team reconcile требует оба флага --before и --after"}
	}
	if open == nil {
		return fmt.Errorf("операции плана не подключены")
	}
	p, err := open(ctx)
	if err != nil {
		return err
	}
	switch verb {
	case "connect":
		err = p.Connect(ctx, remote)
	case "fetch":
		err = p.FetchTeam(ctx)
	case "publish":
		err = p.Publish(ctx)
	case "reconcile":
		err = p.ReconcileTeam(ctx, before, after)
	}
	if err != nil {
		return err
	}
	for _, n := range p.TakeNotices() {
		if _, err := fmt.Fprintf(streams.Err, "Уведомление [%s]: %s\n", n.Code, n.Message); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(streams.Out, "Общий план согласован.")
	return err
}
