package kit

import (
	"context"
	"io"
	"sync"

	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/any-cli/kit/store"
)

// State is the per-run context kit resolves once and shares with every command,
// whether a registered operation or an escape-hatch command added with
// AddCommand. It carries the resolved config, the lazily built domain client,
// and the optional record store. An escape-hatch command reaches it with
// FromContext(cmd.Context()).
type State struct {
	Config  Config
	Globals Globals
	Output  OutputOptions // resolved rendering settings, for escape-hatch commands

	newClient  func(context.Context, Config) (any, error)
	clientOnce sync.Once
	client     any
	clientErr  error

	store store.Store
}

// OutputOptions are the resolved rendering settings an escape-hatch command
// reuses so its structured output matches every operation's: same --output
// format, --fields projection, --no-header, and --template.
type OutputOptions struct {
	Format   string
	Fields   []string
	NoHeader bool
	Template string
	IsTTY    bool
	Width    int
}

// Renderer builds a render.Renderer over w using the run's resolved output
// settings. An escape-hatch command that emits records calls this instead of
// hand-rolling a formatter, then Emit/Flush like any operation sink.
func (s *State) Renderer(w io.Writer) (*render.Renderer, error) {
	return render.New(render.Options{
		Format:   render.Format(s.Output.Format),
		IsTTY:    s.Output.IsTTY,
		Fields:   s.Output.Fields,
		NoHeader: s.Output.NoHeader,
		Template: s.Output.Template,
		Width:    s.Output.Width,
		Writer:   w,
	})
}

// Client builds the domain client on first use and caches it (and any error) for
// the rest of the run. It returns nil when the app registered no client factory.
func (s *State) Client(ctx context.Context) (any, error) {
	if s.newClient == nil {
		return nil, nil
	}
	s.clientOnce.Do(func() {
		s.client, s.clientErr = s.newClient(ctx, s.Config)
	})
	return s.client, s.clientErr
}

// Store returns the open record store for this run, or nil when --db was not
// given. Escape-hatch commands that produce records can tee into it themselves.
func (s *State) Store() store.Store { return s.store }

type stateKey struct{}

// WithState returns a context carrying st, so child commands can retrieve it.
func WithState(ctx context.Context, st *State) context.Context {
	return context.WithValue(ctx, stateKey{}, st)
}

// FromContext returns the run State stored on the context, or nil if absent. An
// escape-hatch command calls this to reach the resolved config, client, and
// store that kit built for the run.
func FromContext(ctx context.Context) *State {
	st, _ := ctx.Value(stateKey{}).(*State)
	return st
}
