// Command git-task provides a local task tracker for Git repositories.
package main

import (
	"context"
	"os"
	"os/signal"

	"git-task/internal/app"
	"git-task/internal/cli"
)

// version can be set at build time with -ldflags=-X=main.version=... .
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return cli.RunWithPlans(ctx, os.Args[1:], version, cli.Streams{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
	}, func(ctx context.Context) (*app.Plans, error) { return app.Open(ctx, "") })
}
