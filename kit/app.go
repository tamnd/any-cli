package kit

import (
	"context"
	"slices"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tamnd/any-cli/kit/store"
)

// Identity is the fixed description of a CLI: its name, what it does, and where
// it lives. It seeds help text, the API title, the MCP server name, and the
// default env prefix.
type Identity struct {
	Binary  string // binary and command name, e.g. "ccrawl"
	Short   string // one-line description
	Long    string // multi-paragraph description for help
	Version string // semantic version, set by the build
	Site    string // the upstream site or service this CLI targets
	Repo    string // source repository URL
}

// App is the registry every surface drives. A domain builds one with New,
// registers operations with Handle, sets its client factory, and hands the App
// to Run. The App holds no surface state of its own; each surface reads the
// operation list and the config to build itself.
type App struct {
	id         Identity
	cfg        Config
	ops        []Operation
	byName     map[string]Operation
	groups     []string
	newCli     func(context.Context, Config) (any, error)
	extra      []*cobra.Command
	nested     []nestedCmd
	parents    map[string]string // parent command name -> its help summary
	openDB     func(context.Context, string) (store.Store, error)
	cfgHook    func(*Config)
	globalHook func(*pflag.FlagSet)
	finalize   func(*Config)
}

// nestedCmd is an escape-hatch command attached under a parent group, so a
// domain can mix kit operations and hand-rolled commands in the same nested
// command (for ccrawl: "crawls list" is an op, "crawls info" is an escape hatch).
type nestedCmd struct {
	parent string
	cmd    *cobra.Command
}

// Option customizes an App at construction.
type Option func(*App)

// EnvPrefix overrides the env var prefix (default: upper-cased binary name).
func EnvPrefix(prefix string) Option {
	return func(a *App) { a.cfg.EnvPrefix = prefix }
}

// WithDefaults lets a domain overlay its own config defaults (rate, retries,
// workers, user agent) onto the framework baseline.
func WithDefaults(fn func(*Config)) Option {
	return func(a *App) { a.cfgHook = fn }
}

// New builds an App from its identity. It derives the baseline config from the
// binary name, then applies any options.
func New(id Identity, opts ...Option) *App {
	a := &App{
		id:      id,
		cfg:     DefaultConfig(id.Binary, ""),
		byName:  map[string]Operation{},
		parents: map[string]string{},
		openDB:  store.Open,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.cfgHook != nil {
		a.cfgHook(&a.cfg)
	}
	return a
}

// SetClient registers the factory kit calls once per run to build the domain
// client from the resolved config. The returned value is injected into handler
// In fields tagged kit:"inject" by assignability. A CLI with no shared client
// can skip this.
func (a *App) SetClient(fn func(context.Context, Config) (any, error)) {
	a.newCli = fn
}

// GlobalFlags lets a domain register its own persistent flags on the root, for
// settings that are not part of the framework baseline (for ccrawl: --crawl,
// --source, --library). The domain binds them to its own variables and reads
// those when building its client and its escape-hatch commands.
func (a *App) GlobalFlags(fn func(*pflag.FlagSet)) {
	a.globalHook = fn
}

// Finalize registers a hook kit calls after folding the framework globals onto
// the config, so a domain can apply its own global flags to the resolved Config
// (for instance copying a --data-dir-derived path into a domain field).
func (a *App) Finalize(fn func(*Config)) {
	a.finalize = fn
}

// AddCommand is the escape hatch for an operation that does not fit the
// emit-records shape (an interactive shell, a DuckDB analytical console, a
// binary download). The command joins the CLI tree as-is. It is absent from the
// API and MCP surfaces, which expose only registered operations.
func (a *App) AddCommand(cmd *cobra.Command) {
	a.extra = append(a.extra, cmd)
}

// AddCommandUnder attaches an escape-hatch command beneath a parent command,
// the same parent a nested operation declares with OpMeta.Parent. It lets a
// domain mix generated operations and hand-rolled commands under one group, so
// "crawls list" (an op) and "crawls info" (an escape hatch) sit side by side.
func (a *App) AddCommandUnder(parent string, cmd *cobra.Command) {
	a.nested = append(a.nested, nestedCmd{parent: parent, cmd: cmd})
}

// CommandGroup sets the help summary for a parent command, whether that parent
// hosts operations, escape-hatch commands, or both. Without it a parent shows a
// generated one-line summary.
func (a *App) CommandGroup(name, summary string) {
	a.parents[name] = summary
}

// Config returns the resolved baseline config, for a domain that needs to read
// a default while wiring its client factory.
func (a *App) Config() Config { return a.cfg }

// Identity returns the app identity.
func (a *App) Identity() Identity { return a.id }

// Ops returns the registered operations in registration order.
func (a *App) Ops() []Operation { return a.ops }

func (a *App) register(o Operation) {
	m := o.Meta()
	if m.Name == "" {
		panic("kit: operation with empty name")
	}
	key := m.key()
	if _, dup := a.byName[key]; dup {
		panic("kit: operation registered twice: " + key)
	}
	a.byName[key] = o
	a.byName[m.toolName()] = o // underscore form, the MCP tool / OpenAPI id
	a.ops = append(a.ops, o)
	for _, al := range m.Aliases {
		a.byName[al] = o
	}
	if m.Group != "" && !slices.Contains(a.groups, m.Group) {
		a.groups = append(a.groups, m.Group)
		sort.Strings(a.groups)
	}
}
