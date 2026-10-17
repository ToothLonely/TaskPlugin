package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"git-task/internal/diagnostic"
)

func runTeam(ctx context.Context, args []string, streams Streams, open OpenPlans) error {
	if len(args) == 0 || len(args) == 1 && args[0] == "--help" {
		return writeHelp(streams.Out, "team")
	}
	verb := args[0]
	if verb != "connect" && verb != "fetch" && verb != "publish" && verb != "reconcile" {
		return &usageError{diagnostic.New("team требует connect, fetch, publish или reconcile")}
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
			return &usageError{diagnostic.Errorf("повтор флага %s", name)}
		}
		seen[name] = true
		if name == "--help" {
			if has {
				return &usageError{diagnostic.New("--help не принимает значение")}
			}
		} else if name == "--remote" && verb == "connect" || verb == "reconcile" && (name == "--before" || name == "--after") {
			if !has {
				i++
				if i == len(args) {
					return &usageError{diagnostic.New("--remote требует имя")}
				}
				value = args[i]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return &usageError{diagnostic.New("неверное имя remote")}
			}
			name += "=" + value
		} else {
			return &usageError{diagnostic.Errorf("неверный аргумент team: %s", args[i])}
		}
		if err := fs.Parse([]string{name}); err != nil {
			return &usageError{err}
		}
	}
	if help {
		return writeHelp(streams.Out, "team")
	}
	if verb == "connect" && remote == "" {
		return &usageError{diagnostic.New("team connect требует --remote <name>")}
	}
	if verb == "reconcile" && (before == "") != (after == "") {
		return &usageError{diagnostic.New("team reconcile требует оба флага --before и --after")}
	}
	if open == nil {
		return diagnostic.Errorf("операции плана не подключены")
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
