package kit

import (
	"context"
	"os"

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

// Main is a convenience wrapper that runs the app with a cancellable context and
// calls os.Exit with the resulting code.
func Main(app *App) {
	ctx := context.Background()
	os.Exit(Run(ctx, app))
}
