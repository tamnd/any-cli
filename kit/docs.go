// Package kit turns one declaration of an operation into three surfaces: a CLI
// subcommand, an HTTP route, and an MCP tool. A domain describes what its verbs
// do and how their inputs and outputs are shaped; kit builds the command tree,
// the flag parser, the JSON-over-HTTP server, and the MCP server from that one
// description.
//
// # The model
//
// An operation is a typed handler with surface-neutral metadata. The handler
// takes a context, a typed input struct, and an emit callback; it streams zero
// or more records to emit and returns an error. kit reflects the input struct
// once to learn its arguments and flags, and the output struct to learn the
// record's collection name and primary key. The same operation then renders as
// a CLI subcommand, answers an HTTP request, and exposes an MCP tool, with no
// per-surface code in the domain.
//
//	type searchIn struct {
//		Query  string        `kit:"arg" help:"URL or prefix to search"`
//		Limit  int           `kit:"flag" default:"20" help:"max captures"`
//		Client *crawl.Client `kit:"inject"`
//	}
//
//	type Capture struct {
//		URL    string `json:"url" kit:"id"`
//		Status int    `json:"status"`
//	}
//
//	kit.Handle(app, kit.OpMeta{
//		Name:    "search",
//		Group:   "read",
//		Summary: "Find captures for a URL or prefix",
//	}, func(ctx context.Context, in searchIn, emit func(Capture) error) error {
//		return in.Client.Search(ctx, in.Query, in.Limit, emit)
//	})
//
// # Input fields
//
// A field's kit tag says how kit fills it:
//
//	kit:"arg"     a positional argument, in declaration order
//	kit:"flag"    a named flag / query parameter / tool argument (the default)
//	kit:"inject"  filled by kit with the domain client or record store
//
// Options after the kind refine the binding:
//
//	name=foo      override the derived snake_case name
//	short=q       single-letter CLI shorthand (-q)
//	variadic      an arg that takes the rest of the positionals; a slice flag
//	inherit       bind to a framework-global flag of the same name (e.g. limit)
//
// Sibling tags describe the value: help for the one-line description, default
// for the unset value, and enum for a comma-separated set of allowed values.
//
// An output field tagged kit:"id" (or named id) is the record's primary key,
// used to upsert into the record store when --db is set.
//
// # Building an app
//
// A domain builds one App, registers its operations, sets a client factory, and
// hands the App to Run:
//
//	func main() {
//		app := kit.New(kit.Identity{
//			Binary:  "ccrawl",
//			Short:   "A command line for Common Crawl",
//			Version: version,
//		})
//		app.SetClient(func(ctx context.Context, c kit.Config) (any, error) {
//			return crawl.New(c), nil
//		})
//		registerOps(app)
//		kit.Main(app)
//	}
//
// New derives the baseline Config (XDG paths, env prefix, rate, retries,
// timeout, workers) from the binary name; WithDefaults overlays the domain's own
// values. SetClient registers the factory kit calls once per run; its result is
// injected into handler fields tagged kit:"inject" by assignability. GlobalFlags
// registers persistent flags that are not part of the baseline, and the domain
// reads them when building its client.
//
// # Escape hatches
//
// Some verbs do not fit the emit-records shape: a byte stream, an interactive
// shell, a bulk download, or a parent that only groups subcommands. A domain
// declares those as a [Command], a small struct of metadata wired to named
// functions, without naming the underlying cobra and pflag types:
//
//	app.AddCommand(kit.Command{
//		Use:   "get <url>",
//		Short: "Print the bytes Common Crawl captured",
//		Args:  kit.ExactArgs(1),
//		Flags: c.flags,
//		Run:   c.run,
//	})
//
// Command.Flags binds flags through a [FlagSet], whose method set mirrors pflag
// (StringVar, StringVarP, IntVar, BoolVar, and so on). Command.Args validates
// the positionals with one of [ExactArgs], [MinimumNArgs], [MaximumNArgs],
// [RangeArgs], or [NoArgs]. A run handler reaches the run's resolved config,
// client, and record store with [MustClient] (or [Client] when building can
// fail), the typed companions to [FromContext] and [State.Client]. Escape-hatch
// commands appear on the CLI only; the HTTP and MCP surfaces expose registered
// operations.
//
// # Surfaces
//
// The default invocation runs the CLI. The serve subcommand starts the HTTP
// server, mcp starts the MCP server over stdio, and each exposes the same
// operations. Run returns a process exit code derived from the error: the
// [github.com/tamnd/any-cli/kit/errs] taxonomy maps a usage error to 2, an empty
// result to 3, a missing credential to 4, a rate limit to 5, and so on, so the
// same failure yields the same code on every surface.
package kit
