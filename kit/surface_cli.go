package kit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/render"
)

// globalFlags holds the framework-wide flags shared by every operation. One
// instance is bound to the root command's persistent flags and read by each
// subcommand's RunE.
type globalFlags struct {
	output   string
	fields   string
	template string
	noHeader bool
	limit    int
	rate     time.Duration
	retries  int
	timeout  time.Duration
	dataDir  string
	noCache  bool
	quiet    bool
	verbose  int
	color    string
	dryRun   bool
	db       string
	profile  string
}

// buildCLI assembles the cobra command tree from the registry: one subcommand
// per operation, grouped by Op.Group, plus the escape-hatch commands and the
// serve and mcp surface switches. Global flags are persistent on the root.
func (a *App) buildCLI() *cobra.Command {
	g := &globalFlags{}
	root := &cobra.Command{
		Use:           a.id.Binary,
		Short:         a.id.Short,
		Long:          a.id.Long,
		Version:       a.id.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.newState(cmd.Context(), g)
			if err != nil {
				return err
			}
			cmd.SetContext(WithState(cmd.Context(), st))
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, _ []string) error {
			if st := FromContext(cmd.Context()); st != nil && st.store != nil {
				return st.store.Close()
			}
			return nil
		},
	}
	bindGlobals(root, g)
	if a.globalHook != nil {
		a.globalHook(&FlagSet{fs: root.PersistentFlags()})
	}

	groups := map[string]*cobra.Group{}
	for _, name := range a.groups {
		grp := &cobra.Group{ID: name, Title: strings.ToUpper(name[:1]) + name[1:] + " commands:"}
		groups[name] = grp
		root.AddGroup(grp)
	}

	// parents holds the lazily created parent command for each distinct
	// OpMeta.Parent, so nested operations and nested escape-hatch commands land
	// in one shared group command.
	parents := map[string]*cobra.Command{}
	parentOf := func(name string) *cobra.Command {
		if p, ok := parents[name]; ok {
			return p
		}
		summary := a.parents[name]
		if summary == "" {
			summary = name + " commands"
		}
		p := &cobra.Command{Use: name, Short: summary}
		parents[name] = p
		root.AddCommand(p)
		return p
	}

	for _, op := range a.ops {
		cmd := a.opCommand(op, g)
		if parent := op.Meta().Parent; parent != "" {
			parentOf(parent).AddCommand(cmd)
		} else {
			root.AddCommand(cmd)
		}
	}
	for _, n := range a.nested {
		parentOf(n.parent).AddCommand(n.cmd.cobraCommand())
	}
	for _, c := range a.extra {
		root.AddCommand(c.cobraCommand())
	}
	root.AddCommand(a.serveCommand(g))
	root.AddCommand(a.mcpCommand())
	return root
}

func bindGlobals(root *cobra.Command, g *globalFlags) {
	f := root.PersistentFlags()
	f.StringVarP(&g.output, "output", "o", "auto", "output format: auto|table|markdown|json|jsonl|csv|tsv|url|raw")
	f.StringVar(&g.fields, "fields", "", "comma-separated columns to show")
	f.StringVar(&g.template, "template", "", "Go template applied per record")
	f.BoolVar(&g.noHeader, "no-header", false, "omit the header row")
	f.IntVarP(&g.limit, "limit", "n", 0, "stop after N records (0 = no limit)")
	f.DurationVar(&g.rate, "rate", 0, "minimum delay between requests")
	f.IntVar(&g.retries, "retries", -1, "retry attempts on rate limit or 5xx")
	f.DurationVar(&g.timeout, "timeout", 0, "per-request timeout")
	f.StringVar(&g.dataDir, "data-dir", "", "override the data directory")
	f.BoolVar(&g.noCache, "no-cache", false, "bypass on-disk caches")
	f.BoolVarP(&g.quiet, "quiet", "q", false, "suppress progress output")
	f.CountVarP(&g.verbose, "verbose", "v", "increase verbosity (repeatable)")
	f.StringVar(&g.color, "color", "auto", "color: auto|always|never")
	f.BoolVar(&g.dryRun, "dry-run", false, "print actions, do not perform them")
	f.StringVar(&g.db, "db", "", "tee every record into a store (e.g. out.db, postgres://...)")
	f.StringVar(&g.profile, "profile", "", "named profile to load")
}

// resolveConfig folds the global flags onto the app baseline.
func (a *App) resolveConfig(g *globalFlags) Config {
	c := a.cfg
	if g.dataDir != "" {
		c.DataDir = g.dataDir
	}
	if g.rate > 0 {
		c.Rate = g.rate
	}
	if g.retries >= 0 {
		c.Retries = g.retries
	}
	if g.timeout > 0 {
		c.Timeout = g.timeout
	}
	c.NoCache = g.noCache
	c.Color = g.color
	c.Quiet = g.quiet
	c.Verbose = g.verbose
	c.DryRun = g.dryRun
	c.DB = g.db
	c.Profile = g.profile
	if a.finalize != nil {
		a.finalize(&c)
	}
	return c
}

// newState resolves the config, wires the lazy client factory, and opens the
// record store if --db was given. It is built once per run in the root's
// PersistentPreRunE and shared with every command through the context.
func (a *App) newState(ctx context.Context, g *globalFlags) (*State, error) {
	// Compile the template once here so a bad --template fails with a clean usage
	// error before any command runs, and so building a renderer over any writer
	// later (operations and escape-hatch commands alike) cannot fail.
	if g.template != "" {
		if _, err := template.New("row").Parse(g.template); err != nil {
			return nil, errs.Usage("bad --template: %v", err)
		}
	}
	st := &State{
		Config:  a.resolveConfig(g),
		Globals: Globals{Limit: g.limit},
		Output: OutputOptions{
			Format:   g.output,
			Fields:   splitList(g.fields),
			NoHeader: g.noHeader,
			Template: g.template,
			IsTTY:    isTTY(os.Stdout),
			Color:    colorEnabled(g.color, isTTY(os.Stdout)),
			Width:    termWidth(),
		},
		newClient: a.newCli,
	}
	if g.db != "" {
		s, err := a.openDB(ctx, g.db)
		if err != nil {
			return nil, errs.Wrap(errs.KindGeneric, err, "open store")
		}
		st.store = s
	}
	return st, nil
}

func (a *App) opCommand(op Operation, g *globalFlags) *cobra.Command {
	m := op.Meta()
	flagVals := map[string]*flagRef{}
	cmd := &cobra.Command{
		Use:     usageLine(m),
		Short:   m.Summary,
		Long:    m.Long,
		Aliases: m.Aliases,
		GroupID: m.Group,
		Args:    argsValidator(op),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := Input{
				Args:    args,
				Flags:   collectFlags(cmd, flagVals),
				Globals: Globals{Limit: g.limit},
			}
			return a.invokeCLI(cmd.Context(), op, in, g)
		},
	}
	for _, p := range op.Params() {
		if p.Kind != KindFlag || p.Inherit {
			continue // inherited flags reuse the persistent global of the same name
		}
		flagVals[p.Name] = registerFlag(cmd, p)
	}
	if m.Write {
		cmd.Annotations = map[string]string{"write": "true"}
	}
	return cmd
}

// invokeCLI reads the shared run state, wires a render sink to stdout, and runs
// the operation. The client and store were resolved once in PersistentPreRunE.
func (a *App) invokeCLI(ctx context.Context, op Operation, in Input, g *globalFlags) error {
	st := FromContext(ctx)
	if st == nil {
		var err error
		st, err = a.newState(ctx, g)
		if err != nil {
			return err
		}
	}
	client, err := st.Client(ctx)
	if err != nil {
		return err
	}

	r, err := st.Renderer(os.Stdout)
	if err != nil {
		return errs.Usage("%v", err)
	}

	rt := RunContext{Client: client, Store: st.store, Limit: g.limit}
	sink := &rendererSink{r: r}
	return op.Invoke(ctx, in, rt, sink)
}

// rendererSink adapts a render.Renderer to the Sink interface.
type rendererSink struct{ r *render.Renderer }

func (s *rendererSink) Emit(rec any) error { return s.r.Emit(rec) }
func (s *rendererSink) Flush() error       { return s.r.Flush() }

func usageLine(m OpMeta) string {
	var b strings.Builder
	b.WriteString(m.Name)
	for _, arg := range m.Args {
		b.WriteByte(' ')
		switch {
		case arg.Variadic:
			b.WriteString("[" + arg.Name + "...]")
		case arg.Optional:
			b.WriteString("[" + arg.Name + "]")
		default:
			b.WriteString("<" + arg.Name + ">")
		}
	}
	return b.String()
}

func argsValidator(op Operation) cobra.PositionalArgs {
	m := op.Meta()
	required := 0
	variadic := false
	for _, arg := range m.Args {
		if arg.Variadic {
			variadic = true
		}
		if !arg.Optional && !arg.Variadic {
			required++
		}
	}
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < required {
			return errs.Usage("expected at least %d argument(s), got %d", required, len(args))
		}
		if !variadic && len(args) > len(m.Args) {
			return errs.Usage("expected at most %d argument(s), got %d", len(m.Args), len(args))
		}
		return nil
	}
}

func exitCodeFor(err error) int {
	return errs.ExitCode(err)
}

// colorEnabled resolves the --color flag against the terminal and the NO_COLOR
// convention. auto colors only an interactive terminal; always forces color on
// (e.g. piping into a pager that interprets ANSI); never disables it. This is
// what keeps `cmd | jq` and other scripted pipes plain: a pipe is not a TTY, so
// auto resolves to no color.
func colorEnabled(mode string, tty bool) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return tty && os.Getenv("NO_COLOR") == ""
	}
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func termWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 0
}
