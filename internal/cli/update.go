package cli

import (
	"context"

	"git-task/internal/diagnostic"
	"git-task/internal/update"
)

func runUpdate(ctx context.Context, args []string, version string, streams Streams) error {
	positionals, help, err := parseInfoArgs("update", args)
	if err != nil {
		return err
	}
	if len(positionals) != 0 {
		return &usageError{diagnostic.New("update не принимает позиционные аргументы")}
	}
	if help {
		return writeHelp(streams.Out, "update")
	}
	run := streams.RunUpdate
	if run == nil {
		run = update.Run
	}
	if err := run(ctx, version, streams.Out, streams.Err); err != nil {
		return diagnostic.Errorf("обновление не подтверждено: %w", err)
	}
	return nil
}
