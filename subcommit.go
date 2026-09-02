package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/reemus-dev/subcommit/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx); err != nil {
		return 1
	}
	return 0
}
