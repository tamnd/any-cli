package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/any-cli/scaffold"
)

func newNewCmd() *cobra.Command {
	var (
		dir     string
		opt     scaffold.Options
		noGit   bool
		noTidy  bool
		remote  bool
		private bool
	)

	cmd := &cobra.Command{
		Use:   "new <site>",
		Short: "Scaffold a new <site>-cli repository",
		Long: `new writes a complete <site>-cli repository: the command tree, a library
package, GoReleaser config, the ci/release/docs workflows, a tago docs site, and
the house style files. The result builds and releases as-is; you then fill in the
site-specific commands.

  any new reddit
  any new reddit --remote --short "A command line for Reddit."
  any new lobsters -d ~/code --binary lob`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			if name == "" || strings.ContainsAny(name, "/ \t") {
				return fmt.Errorf("site name %q must be a single bare word, e.g. reddit", args[0])
			}
			site := scaffold.NewSite(name, opt)

			parent := dir
			if parent == "" {
				parent = "."
			}
			destDir := filepath.Join(parent, site.Repo)
			if entries, err := os.ReadDir(destDir); err == nil && len(entries) > 0 {
				return fmt.Errorf("%s already exists and is not empty", destDir)
			}

			out := c.OutOrStdout()
			fmt.Fprintf(out, "Scaffolding %s into %s\n", site.Module, destDir)

			files, err := scaffold.Render(destDir, site)
			if err != nil {
				return fmt.Errorf("render: %w", err)
			}
			fmt.Fprintf(out, "Wrote %d files\n", len(files))

			if !noGit {
				msg := fmt.Sprintf("Add %s, a command line for %s", site.Binary, site.Name)
				if err := scaffold.InitGit(out, destDir, msg); err != nil {
					fmt.Fprintf(out, "note: git init skipped (%v)\n", err)
				}
			}
			if !noTidy {
				if err := scaffold.Tidy(out, destDir); err != nil {
					fmt.Fprintf(out, "note: go mod tidy did not run (%v); run it before building\n", err)
				}
			}
			if remote {
				if err := scaffold.CreateRemote(out, destDir, site.Owner, site.Repo, private); err != nil {
					return fmt.Errorf("create remote: %w", err)
				}
			}

			printNextSteps(out, site, destDir, remote)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&dir, "dir", "d", "", "parent directory for the new repo (default current directory)")
	f.StringVar(&opt.Owner, "owner", "", "GitHub owner (default tamnd)")
	f.StringVar(&opt.Binary, "binary", "", "command/binary name (default the site name)")
	f.StringVarP(&opt.Short, "short", "s", "", "one-line description")
	f.StringVar(&opt.Description, "description", "", "longer description (default the short one)")
	f.StringVar(&opt.License, "license", "", "SPDX license id (default Apache-2.0)")
	f.StringVar(&opt.Author, "author", "", "copyright holder (default Duc-Tam Nguyen)")
	f.StringVar(&opt.Email, "email", "", "maintainer email (default tamnd87@gmail.com)")
	f.BoolVar(&noGit, "no-git", false, "do not initialise a git repo or add the docs theme")
	f.BoolVar(&noTidy, "no-tidy", false, "do not run go mod tidy")
	f.BoolVar(&remote, "remote", false, "create the GitHub repo with gh and push")
	f.BoolVar(&private, "private", false, "with --remote, create the repo private")
	return cmd
}

func printNextSteps(out interface{ Write([]byte) (int, error) }, site scaffold.Site, destDir string, remote bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "\nDone. %s is ready.\n\n", site.Repo)
	fmt.Fprintf(&b, "Next:\n")
	fmt.Fprintf(&b, "  cd %s\n", destDir)
	fmt.Fprintf(&b, "  make build && ./bin/%s version\n", site.Binary)
	fmt.Fprintf(&b, "  # implement the site commands in cli/ on top of the %s/ library\n", site.LibPkg)
	if !remote {
		fmt.Fprintf(&b, "\nWhen ready to publish:\n")
		fmt.Fprintf(&b, "  gh repo create %s/%s --public --source=. --remote=origin --push\n", site.Owner, site.Repo)
	}
	fmt.Fprintf(&b, "\nTo release, push a tag:\n")
	fmt.Fprintf(&b, "  git tag v0.1.0 && git push --tags   # GoReleaser builds every artifact\n")
	out.Write([]byte(b.String()))
}
