// Package cli builds the any command tree on top of the scaffold engine.
package cli

import (
	"github.com/spf13/cobra"
)

// Build metadata, set via -ldflags at release time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Root builds the root command and its subtree.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "any",
		Short: "Scaffold a new tamnd/*-cli repository, fully wired",
		Long: `any creates a new site CLI repository from one proven template.

Every tamnd/*-cli repo shares the same skeleton: a cobra command tree, a pure-Go
library package, GoReleaser cross-builds with packages and a container image, the
ci/release/docs GitHub Actions, a tago documentation site, and the house style.
"any new <site>" writes all of it at once, so a new CLI starts complete instead
of accreting boilerplate by hand.

Quick start:
  any new reddit                       scaffold ./reddit-cli, ready to build
  any new reddit --remote              also create tamnd/reddit-cli and push
  any new lobsters --short "..." -d ~/code   custom blurb and parent directory`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newNewCmd())
	root.AddCommand(newVersionCmd())
	return root
}
