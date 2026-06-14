# kit

Declare an operation once, expose it as a CLI subcommand, an HTTP route, and an
MCP tool. A domain describes what its verbs do and how their inputs and outputs
are shaped; kit builds the command tree, the flag parser, the JSON-over-HTTP
server, and the MCP server from that one description. The domain imports kit and
its own data package, and nothing else: cobra, pflag, and fang stay behind the
framework.

## The shape of an operation

An operation is a typed handler plus surface-neutral metadata. The handler takes
a context, a typed input struct, and an `emit` callback; it streams zero or more
records and returns an error.

```go
type searchIn struct {
	Query  string        `kit:"arg" help:"URL or prefix to search"`
	Limit  int           `kit:"flag" default:"20" help:"max captures"`
	Client *crawl.Client `kit:"inject"`
}

type Capture struct {
	URL    string `json:"url" kit:"id"`
	Status int    `json:"status"`
}

kit.Handle(app, kit.OpMeta{
	Name:    "search",
	Group:   "read",
	Summary: "Find captures for a URL or prefix",
}, func(ctx context.Context, in searchIn, emit func(Capture) error) error {
	return in.Client.Search(ctx, in.Query, in.Limit, emit)
})
```

That one registration yields:

- `mybin search example.com --limit 50` on the CLI,
- `GET /search?q=example.com&limit=50` once you run `mybin serve`,
- a `search` MCP tool once you run `mybin mcp`,

each rendering the same `Capture` stream, applying the same `--limit`, and
mapping the same errors to the same exit codes and HTTP statuses.

`kit.Handle` is the only registration call a domain makes. It reflects the input
struct once to build the parameter set, and the output struct to learn the
record collection name and its `kit:"id"` primary key (used to upsert into the
record store when `--db` is set).

## Input fields

The `kit` struct tag says how kit fills a field:

| tag | meaning |
| --- | --- |
| `kit:"arg"` | a positional argument, in declaration order |
| `kit:"flag"` | a named flag / query parameter / tool argument (the default) |
| `kit:"inject"` | filled by kit with the domain client or the record store |

Options after the kind refine the binding:

| option | effect |
| --- | --- |
| `name=foo` | override the derived snake_case name |
| `short=q` | single-letter CLI shorthand (`-q`) |
| `variadic` | an arg that takes the rest of the positionals; a slice flag |
| `inherit` | bind to a framework-global flag of the same name (e.g. `limit`) |

Sibling tags describe the value:

```go
Mode  string `kit:"flag" short:"m" default:"text" enum:"text,html,raw" help:"output mode"`
Hosts []string `kit:"arg" variadic help:"one or more hosts"`
```

`help` is the one-line description shown in CLI help, the OpenAPI summary, and
the MCP schema. `default` is the value used when the flag is unset. `enum` is a
comma-separated set of allowed values.

An `inject` field receives the run's domain client or its record store by
assignability, so a handler declares `Client *crawl.Client` or `St store.Store`
and gets the right one.

## Building an app

```go
func main() {
	app := kit.New(kit.Identity{
		Binary:  "ccrawl",
		Short:   "A command line for Common Crawl",
		Version: version,
	})

	app.SetClient(func(ctx context.Context, c kit.Config) (any, error) {
		return crawl.New(c), nil
	})

	registerOps(app)
	kit.Main(app)
}
```

- **`kit.New(Identity, ...Option)`** derives the baseline `Config` (XDG data and
  config dirs, env prefix, rate, retries, timeout, workers) from the binary
  name. `kit.WithDefaults(fn)` overlays the domain's own values onto that
  baseline; `kit.EnvPrefix("CC")` overrides the env var prefix.
- **`app.SetClient(fn)`** registers the factory kit calls once per run to build
  the domain client from the resolved `Config`. The result is injected into
  handler `kit:"inject"` fields. A CLI with no shared client can skip this.
- **`app.GlobalFlags(fn)`** registers persistent flags that are not part of the
  baseline (for ccrawl: `--crawl`, `--source`). The domain binds them to its own
  variables through a `kit.FlagSet` and reads them when building its client.
- **`kit.Run(ctx, app)`** builds the CLI tree, runs it, and returns a process
  exit code. **`kit.Main(app)`** wraps that with a cancellable context and
  `os.Exit`.

Wire the hooks as named methods rather than closures when they need shared
state, which keeps each one readable as package-level behaviour:

```go
type builder struct {
	dom *domainGlobals
	def crawl.Config
}

func (b *builder) defaults(c *kit.Config) { c.Rate = b.def.Delay /* ... */ }
func (b *builder) globals(f *kit.FlagSet)  { f.StringVarP(&b.dom.crawl, "crawl", "c", "latest", "crawl ID") }
func (b *builder) client(_ context.Context, c kit.Config) (any, error) { return buildApp(c, b.dom), nil }

b := &builder{dom: &domainGlobals{}, def: crawl.DefaultConfig()}
app := kit.New(id, kit.WithDefaults(b.defaults))
app.GlobalFlags(b.globals)
app.SetClient(b.client)
```

## Escape hatches

Some verbs do not fit the emit-records shape: a byte stream, an interactive
shell, a bulk download, or a parent that only groups subcommands. Declare those
as a `kit.Command`, a small struct of metadata wired to named functions:

```go
type getCmd struct {
	mode    string
	outFile string
}

func newGetCmd() kit.Command {
	c := &getCmd{}
	return kit.Command{
		Use:   "get <url>",
		Short: "Print the bytes Common Crawl captured",
		Args:  kit.ExactArgs(1),
		Flags: c.flags,
		Run:   c.run,
	}
}

func (c *getCmd) flags(f *kit.FlagSet) {
	f.StringVarP(&c.mode, "mode", "m", "text", "output mode")
	f.StringVarP(&c.outFile, "out", "O", "", "write to a file")
}

func (c *getCmd) run(ctx context.Context, args []string) error {
	app := kit.MustClient[*App](ctx)
	return runGet(ctx, app, args[0], c.mode, c.outFile)
}

app.AddCommand(newGetCmd())
```

- **`Command.Flags func(*FlagSet)`** binds flags through a `kit.FlagSet`, whose
  method set mirrors pflag: `StringVar`/`StringVarP`, `StringSliceVar`,
  `IntVar`/`Int64Var`, `BoolVar`, `Float64Var`, `DurationVar`, `CountVar`, each
  with a `...P` shorthand variant.
- **`Command.Args Args`** validates the positionals. Use `kit.ExactArgs(n)`,
  `kit.MinimumNArgs(n)`, `kit.MaximumNArgs(n)`, `kit.RangeArgs(min, max)`, or
  `kit.NoArgs`. They return a usage error, so a wrong argument count exits 2 with
  a message on every surface.
- **`Command.Sub []Command`** nests subcommands. **`Command.Write bool`** marks a
  mutating command. **`Command.Group`** places the command in a help group.
- **`app.AddCommandUnder(parent, cmd)`** attaches an escape hatch beneath a
  parent that also hosts operations, so `crawls list` (an op) and `crawls info`
  (an escape hatch) sit side by side.

A run handler reaches the run's resolved config, client, and record store with
the context:

```go
app := kit.MustClient[*App](ctx)        // panics on a wiring bug; for infallible factories
app, err := kit.Client[*App](ctx)       // returns the error; for factories that can fail
```

`kit.MustClient` mirrors `template.Must` and `regexp.MustCompile`: when a client
factory only assembles config and shared handles, a missing or mistyped client
is a wiring bug, not a runtime condition. For lower-level access, `kit.FromContext(ctx)`
returns the full `*kit.State` (config, client, store, resolved output options).

Escape-hatch commands appear on the CLI only. The HTTP and MCP surfaces expose
registered operations.

## Surfaces and exit codes

The default invocation runs the CLI. `mybin serve` starts the HTTP server and
`mybin mcp` starts the MCP server over stdio, both exposing the same operations.

Return one of the `kit/errs` errors to get a stable result across surfaces:

| constructor | exit code | HTTP status | meaning |
| --- | --- | --- | --- |
| (nil) | 0 | 200 | success |
| `errs.Usage` | 2 | 400 | bad arguments or flags |
| `errs.NoResults` | 3 | 404 | the stream was empty |
| `errs.NeedAuth` | 4 | 401 | a credential is required |
| `errs.RateLimited` | 5 | 429 | upstream throttled the request |
| `errs.NotFound` | 6 | 404 | the requested thing does not exist |
| `errs.Unsupported` | 7 | 422 | the operation cannot be served here |
| `errs.Network` | 8 | 502 | an upstream call failed |
| any other error | 1 | 500 | unclassified failure |

An operation that emits nothing returns `NoResults` automatically (exit 3),
unless its `OpMeta.Single` is set.

## Package layout

- `kit` — the registry, the surfaces, the `Command` builder, `Client`/`MustClient`.
- `kit/errs` — the error taxonomy and its exit-code and HTTP-status mapping.
- `kit/render` — the record renderer (`--output` table/markdown/json/jsonl/csv/tsv/url/raw/template; table and JSON are color-aware on a TTY).
- `kit/store` — the optional record store written to when `--db` is set.

See [docs.go](docs.go) for the package overview rendered by `go doc`.
