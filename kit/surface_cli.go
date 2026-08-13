package kit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/charmbracelet/x/term"
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
		// cobra checks the root's arguments inside Find, before the command
		// runs, and the error it makes there is a plain one worth exit 1. Taking
		// the check gives the same mistake the same exit code at every level.
		// ArbitraryArgs is what turns that check off; unknownOrHelp is what puts
		// it back.
		Args: cobra.ArbitraryArgs,
		RunE: unknownOrHelp,
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
	// A flag the parser rejects is a usage error like any other, but cobra hands
	// it back as a plain error and it came out as exit 1 while every mistake kit
	// catches itself came out as exit 2. The func is inherited, so setting it on
	// the root covers every subcommand.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return errs.Usage("%s", err.Error())
	})
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
		p := &cobra.Command{Use: name, Short: summary, RunE: unknownOrHelp}
		parents[name] = p
		root.AddCommand(p)
		return p
	}

	for _, op := range a.ops {
		if op.Meta().NoCLI {
			continue
		}
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

// unknownOrHelp is what a command that only holds other commands does when it
// is reached with nothing to run. No arguments is a request for help, so it
// prints help on stdout and exits 0. Anything else is a word that matched no
// subcommand, which is a mistake, and it is answered on stderr with exit 2.
//
// It exists because a command with no RunE is not runnable, and cobra returns
// flag.ErrHelp for a command that is not runnable before it ever validates the
// arguments. So Args: cobra.NoArgs on a group command does nothing at all:
// "host lst -o jsonl > hosts.jsonl" wrote help into the file and exited 0, and
// nothing in the exit code or on stderr said the list was not a list. Giving the
// command a RunE is what gets it far enough to have an opinion.
func unknownOrHelp(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	// cobra offers suggestions for the same mistake at the top level and they
	// are usually right, so they are worth keeping. On one line rather than the
	// list cobra prints, because this goes through fang's error box, and with no
	// question mark on the end because fang appends a full stop to whatever it
	// is given.
	//
	// The distance has to be set here. cobra defaults it in findSuggestions,
	// which is unexported, so the exported SuggestionsFor sees zero and matches
	// only an exact alias, which is to say nothing.
	if !cmd.DisableSuggestions {
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		s := cmd.SuggestionsFor(args[0])
		// All of them when there are several, rather than the first in command
		// order. SuggestionsFor does not rank them, so picking one would be
		// picking whichever happened to be registered earliest.
		for i, name := range s {
			switch {
			case i == 0:
				msg += fmt.Sprintf(", did you mean %q", name)
			case i == len(s)-1:
				msg += fmt.Sprintf(" or %q", name)
			default:
				msg += fmt.Sprintf(", %q", name)
			}
		}
	}
	return errs.Usage("%s", msg)
}

// outputFlagHelp lists the built-in output formats plus any a binary added
// through render.RegisterEncoder, so the --output help advertises only what
// this binary can actually produce.
func outputFlagHelp() string {
	base := "output format: auto|table|markdown|list|json|jsonl|csv|tsv|url|raw"
	for _, f := range render.RegisteredFormats() {
		base += "|" + string(f)
	}
	return base
}

func bindGlobals(root *cobra.Command, g *globalFlags) {
	f := root.PersistentFlags()
	f.StringVarP(&g.output, "output", "o", "auto", outputFlagHelp())
	f.StringVar(&g.fields, "fields", "", "comma-separated columns to show")
	f.StringVar(&g.template, "template", "", "Go template applied per record")
	f.BoolVar(&g.noHeader, "no-header", false, "omit the header row")
	f.IntVarP(&g.limit, "limit", "n", 0, "stop after N records (0 = no limit)")
	f.DurationVar(&g.rate, "rate", 0, "minimum delay between requests")
	f.IntVar(&g.retries, "retries", -1, "retry attempts on rate limit or 5xx (-1 uses the built-in default)")
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

// termWidth reports the terminal width in columns, or 0 when stdout is not a
// terminal (a pipe or file). The renderer uses it to shrink a too-wide table to
// fit; a 0 leaves output at its natural width, which is what a pipe wants.
// COLUMNS wins when set so the width is scriptable and testable.
func termWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 0
}
