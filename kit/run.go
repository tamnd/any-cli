package kit

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/fang"
)

// Run is the single entrypoint a generated main calls. It builds the CLI tree
// from the registry, wraps it in fang for help, completion, and error
// rendering, and executes. The default invocation is the CLI; the serve, mcp,
// and tui subcommands switch surfaces. Run returns the process exit code.
func Run(ctx context.Context, app *App) int {
	root := app.buildCLI()
	opts := []fang.Option{
		fang.WithVersion(app.id.Version),
	}
	if err := fang.Execute(ctx, root, opts...); err != nil {
		return exitCodeFor(err)
	}
	return 0
}

// Main is a convenience wrapper that runs the app with a context cancelled on
// the first interrupt (Ctrl-C) or SIGTERM, then calls os.Exit with the resulting
// code. The serve and mcp surfaces watch this context, so a signal shuts them
// down cleanly instead of killing the process mid-write.
func Main(app *App) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := Run(ctx, app)
	stop()
	os.Exit(code)
}
