package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"git-task/internal/git"
	"git-task/internal/hooks"
)

func runHooks(ctx context.Context, args []string, streams Streams) error {
	positionals, help, err := parseInfoArgs("hooks", args)
	if err != nil {
		return err
	}
	if len(positionals) > 1 || len(positionals) == 1 && positionals[0] != "install" && positionals[0] != "uninstall" {
		return &usageError{"hooks требует install или uninstall"}
	}
	if help {
		return writeHelp(streams.Out, "hooks")
	}
	if len(positionals) != 1 {
		return &usageError{"hooks требует install или uninstall"}
	}
	client, err := git.New("")
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	i := hooks.Installer{Git: client, Binary: binary}
	var changed bool
	if positionals[0] == "install" {
		changed, err = i.Install(ctx)
	} else {
		changed, err = i.Uninstall(ctx)
	}
	if err != nil {
		return err
	}
	message := "Hooks уже в требуемом состоянии."
	if changed && positionals[0] == "install" {
		message = "Hooks установлены."
	} else if changed {
		message = "Hooks удалены; локальный план сохранён."
	}
	_, err = fmt.Fprintln(streams.Out, message)
	return err
}

func runHook(ctx context.Context, args []string, streams Streams, open OpenPlans) {
	if os.Getenv("GIT_TASK_OPERATION") != "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(args) == 0 {
			return fmt.Errorf("не указано событие hook")
		}
		if err := hooks.Validate(args[0], args[1:], streams.In); err != nil {
			return err
		}
		if open == nil {
			return fmt.Errorf("tracking недоступен")
		}
		plans, err := open(ctx)
		if err != nil {
			return err
		}
		sync := plans.Sync
		if args[0] == "post-merge" && args[1] == "0" {
			sync = plans.SyncAfterMerge
		}
		plan, _, err := sync(ctx)
		if err != nil {
			return err
		}
		return writeWarnings(streams.Err, plan)
	}()
	if err != nil {
		fmt.Fprintf(streams.Err, "Предупреждение git-task: tracking не выполнен: %v. Git-операция уже произошла; выполните git task sync.\n", err)
	}
}
