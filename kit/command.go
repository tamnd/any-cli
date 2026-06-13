package kit

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tamnd/any-cli/kit/errs"
)

// Command is a hand-written escape-hatch command, declared without naming the
// underlying cobra/pflag types. A domain reaches for it when a verb does not fit
// the emit-records shape of an operation: a byte stream, an interactive shell, a
// bulk download, or a parent that only groups subcommands.
//
// The function-typed fields take named methods or functions, not closures, so a
// command reads as a small struct of metadata wired to package-level behaviour:
//
//	func newPathsCmd() kit.Command {
//		c := &pathsCmd{}
//		return kit.Command{
//			Use:   "paths <kind>",
//			Short: "List the archive file paths for a crawl",
//			Args:  kit.MaximumNArgs(1),
//			Flags: c.flags,
//			Run:   c.run,
//		}
//	}
//
// Escape-hatch commands appear on the CLI only; the HTTP and MCP surfaces expose
// registered operations.
type Command struct {
	Use     string                                // usage line, e.g. "paths <kind>"
	Short   string                                // one-line summary
	Long    string                                // multi-paragraph help
	Aliases []string                              // alternate names
	Group   string                                // help group ID, matching an operation's Group
	Args    Args                                  // positional-argument check; nil accepts any
	Flags   func(*FlagSet)                        // binds this command's flags; nil for none
	Run     func(context.Context, []string) error // handler; nil for a parent
	Sub     []Command                             // subcommands
	Write   bool                                  // marks a mutating command (annotated for surfaces)
}

// cobraCommand converts a Command into the cobra command kit drives internally.
func (c Command) cobraCommand() *cobra.Command {
	cc := &cobra.Command{
		Use:     c.Use,
		Short:   c.Short,
		Long:    c.Long,
		Aliases: c.Aliases,
		GroupID: c.Group,
	}
	if c.Args != nil {
		cc.Args = c.Args.positional()
	}
	if run := c.Run; run != nil {
		cc.RunE = func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), args)
		}
	}
	if c.Flags != nil {
		c.Flags(&FlagSet{fs: cc.Flags()})
	}
	if c.Write {
		cc.Annotations = map[string]string{"write": "true"}
	}
	for _, sub := range c.Sub {
		cc.AddCommand(sub.cobraCommand())
	}
	return cc
}

// Args validates a command's positional arguments. The constructors below cover
// the common cases; any function with this signature works, and returning an
// errs.Usage error keeps the exit code consistent with the rest of kit.
type Args func([]string) error

func (a Args) positional() cobra.PositionalArgs {
	if a == nil {
		return nil
	}
	return func(_ *cobra.Command, args []string) error { return a(args) }
}

// ExactArgs requires exactly n positional arguments.
func ExactArgs(n int) Args {
	return func(args []string) error {
		if len(args) != n {
			return errs.Usage("expected exactly %d argument(s), got %d", n, len(args))
		}
		return nil
	}
}

// MinimumNArgs requires at least n positional arguments.
func MinimumNArgs(n int) Args {
	return func(args []string) error {
		if len(args) < n {
			return errs.Usage("expected at least %d argument(s), got %d", n, len(args))
		}
		return nil
	}
}

// MaximumNArgs allows at most n positional arguments.
func MaximumNArgs(n int) Args {
	return func(args []string) error {
		if len(args) > n {
			return errs.Usage("expected at most %d argument(s), got %d", n, len(args))
		}
		return nil
	}
}

// RangeArgs requires between min and max positional arguments, inclusive.
func RangeArgs(min, max int) Args {
	return func(args []string) error {
		if len(args) < min || len(args) > max {
			return errs.Usage("expected %d to %d argument(s), got %d", min, max, len(args))
		}
		return nil
	}
}

// NoArgs rejects any positional argument.
func NoArgs(args []string) error {
	if len(args) > 0 {
		return errs.Usage("expected no arguments, got %d", len(args))
	}
	return nil
}

// FlagSet binds a command's flags to a domain's own variables, the way a
// flag.FlagSet does, but without exposing the cobra/pflag types to the domain.
// The method set mirrors pflag one-for-one: each Var binds a long flag, and the
// matching VarP variant adds a single-letter shorthand. A domain that needs a
// flag type not listed here can request one; the set covers the common cases.
type FlagSet struct{ fs *pflag.FlagSet }

// StringVar binds a string flag.
func (f *FlagSet) StringVar(p *string, name, value, usage string) {
	f.fs.StringVar(p, name, value, usage)
}

// StringVarP binds a string flag with a shorthand.
func (f *FlagSet) StringVarP(p *string, name, shorthand, value, usage string) {
	f.fs.StringVarP(p, name, shorthand, value, usage)
}

// StringSliceVar binds a repeatable string flag, collected into a slice.
func (f *FlagSet) StringSliceVar(p *[]string, name string, value []string, usage string) {
	f.fs.StringSliceVar(p, name, value, usage)
}

// StringSliceVarP binds a repeatable string flag with a shorthand.
func (f *FlagSet) StringSliceVarP(p *[]string, name, shorthand string, value []string, usage string) {
	f.fs.StringSliceVarP(p, name, shorthand, value, usage)
}

// IntVar binds an int flag.
func (f *FlagSet) IntVar(p *int, name string, value int, usage string) {
	f.fs.IntVar(p, name, value, usage)
}

// IntVarP binds an int flag with a shorthand.
func (f *FlagSet) IntVarP(p *int, name, shorthand string, value int, usage string) {
	f.fs.IntVarP(p, name, shorthand, value, usage)
}

// Int64Var binds an int64 flag.
func (f *FlagSet) Int64Var(p *int64, name string, value int64, usage string) {
	f.fs.Int64Var(p, name, value, usage)
}

// Int64VarP binds an int64 flag with a shorthand.
func (f *FlagSet) Int64VarP(p *int64, name, shorthand string, value int64, usage string) {
	f.fs.Int64VarP(p, name, shorthand, value, usage)
}

// BoolVar binds a bool flag.
func (f *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	f.fs.BoolVar(p, name, value, usage)
}

// BoolVarP binds a bool flag with a shorthand.
func (f *FlagSet) BoolVarP(p *bool, name, shorthand string, value bool, usage string) {
	f.fs.BoolVarP(p, name, shorthand, value, usage)
}

// Float64Var binds a float64 flag.
func (f *FlagSet) Float64Var(p *float64, name string, value float64, usage string) {
	f.fs.Float64Var(p, name, value, usage)
}

// Float64VarP binds a float64 flag with a shorthand.
func (f *FlagSet) Float64VarP(p *float64, name, shorthand string, value float64, usage string) {
	f.fs.Float64VarP(p, name, shorthand, value, usage)
}

// DurationVar binds a time.Duration flag (e.g. --timeout 30s).
func (f *FlagSet) DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	f.fs.DurationVar(p, name, value, usage)
}

// DurationVarP binds a time.Duration flag with a shorthand.
func (f *FlagSet) DurationVarP(p *time.Duration, name, shorthand string, value time.Duration, usage string) {
	f.fs.DurationVarP(p, name, shorthand, value, usage)
}

// CountVar binds a repeatable counting flag (e.g. --verbose --verbose).
func (f *FlagSet) CountVar(p *int, name, usage string) {
	f.fs.CountVar(p, name, usage)
}

// CountVarP binds a repeatable counting flag with a shorthand (e.g. -vvv).
func (f *FlagSet) CountVarP(p *int, name, shorthand, usage string) {
	f.fs.CountVarP(p, name, shorthand, usage)
}
