// Command any scaffolds a new tamnd/*-cli repository with the full release,
// docs, and CI setup already wired in, so a new site CLI starts from a
// complete, building, releasable skeleton instead of a blank directory.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/tamnd/any-cli/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cli.Root()
	// fang gives styled help, errors, and shell completion for free; the command
	// tree and its exit-code mapping stay in the cli package.
	if err := fang.Execute(ctx, root,
		fang.WithVersion(cli.Version),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	); err != nil {
		os.Exit(1)
	}
}
