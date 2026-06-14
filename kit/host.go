package kit

import (
	"context"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/store"
)

// Host is a multi-domain resolver: it mounts every registered Domain behind its
// URI scheme and dereferences a resource URI to a record by routing to that
// domain's operations. It is the runtime ant drives, and the analogue of an
// *sql.DB that can talk to every registered driver at once. A Host is safe for
// concurrent use; clients are built lazily and cached per domain.
type Host struct {
	mounts map[string]*mount    // canonical scheme -> mounted domain
	mint   map[reflect.Type]tpl // Out type -> how to mint its URI
}

// tpl records how to turn a record of a given type into its URI: the scheme and
// authority that name it, and where its id lives.
type tpl struct {
	scheme    string
	authority string
	idIdx     []int
}

// mount is one domain wired into a host: its driver, its built App (the same one
// the single-site binary uses), the config its client is built from, and the op
// indexes that map a URI authority to the op that dereferences or lists it.
type mount struct {
	dom  Domain
	info DomainInfo
	app  *App
	cfg  Config

	resolvers map[string]Operation // authority -> single-record dereference op
	picked    map[string]bool      // authority chosen via OpMeta.Resolver
	lists     map[string]Operation // authority -> member-list op
	search    Operation            // free-text query op, if the domain has one

	once   sync.Once
	client any
	cerr   error
	store  store.Store
}

// HostOption customizes a Host at Open.
type HostOption func(*hostConfig)

type hostConfig struct {
	tune  map[string]func(*Config) // per-scheme config overlay
	store store.Store
}

// Tune overlays extra config onto one domain before its client is built, keyed
// by canonical scheme. It is how a host points a domain at a shared data dir or
// flips a domain-specific Extra setting without reaching into the driver.
func Tune(scheme string, fn func(*Config)) HostOption {
	return func(hc *hostConfig) {
		if hc.tune == nil {
			hc.tune = map[string]func(*Config){}
		}
		hc.tune[scheme] = fn
	}
}

// WithStore tees every dereferenced record into st, so a walk of the graph fills
// a local store as a side effect, exactly as a single-site read does with --db.
func WithStore(st store.Store) HostOption {
	return func(hc *hostConfig) { hc.store = st }
}

// Open mounts every registered domain and returns a ready Host. It builds each
// domain's App once (so op metadata and client factories are resolved up front)
// but defers building clients until a URI actually routes to that domain. It is
// the analogue of sql.Open, except the data source is the whole registry rather
// than one named driver.
func Open(opts ...HostOption) (*Host, error) {
	var hc hostConfig
	for _, opt := range opts {
		opt(&hc)
	}
	h := &Host{
		mounts: map[string]*mount{},
		mint:   map[reflect.Type]tpl{},
	}
	for _, scheme := range Domains() {
		dom, _ := Lookup(scheme)
		info := dom.Info()
		app := New(info.Identity)
		dom.Register(app)
		cfg := app.cfg
		if fn := hc.tune[scheme]; fn != nil {
			fn(&cfg)
		}
		m := &mount{
			dom:       dom,
			info:      info,
			app:       app,
			cfg:       cfg,
			resolvers: map[string]Operation{},
			picked:    map[string]bool{},
			lists:     map[string]Operation{},
			store:     hc.store,
		}
		h.index(scheme, m)
		h.mounts[scheme] = m
	}
	return h, nil
}

// index walks a mount's ops and files each one under the URI authority it serves,
// so a URI's authority selects the op that mints that record type.
func (h *Host) index(scheme string, m *mount) {
	for _, op := range m.app.ops {
		meta := op.Meta()
		// A free-text search op is keyed by its verb, not by a URI authority: its
		// results are named by their own record ids, not by the query string, so it
		// is not URI-addressable. Capture it here, before the OutType gate, so a
		// host can still drive a domain's search even when it emits a bare any.
		if m.search == nil && meta.Name == "search" && meta.Parent == "" {
			m.search = op
		}
		t := op.OutType()
		if t == nil {
			continue
		}
		authority := meta.URIType
		if authority == "" {
			authority = strings.ToLower(t.Name())
		}
		switch {
		case meta.List:
			// A list op's authority is the parent resource it enumerates, not the
			// type it emits (a series lists SeriesBook rows), so it never seeds the
			// mint index; only a resolver op names its own record type.
			if _, ok := m.lists[authority]; !ok {
				m.lists[authority] = op
			}
		default:
			// A later op marked Resolver wins; otherwise first registration wins,
			// so a domain controls which op is canonical for an authority.
			if _, ok := m.resolvers[authority]; !ok || (meta.Resolver && !m.picked[authority]) {
				m.resolvers[authority] = op
				if meta.Resolver {
					m.picked[authority] = true
				}
			}
			if _, ok := h.mint[t]; !ok {
				h.mint[t] = tpl{scheme: scheme, authority: authority, idIdx: idFieldIndex(t)}
			}
		}
	}
}

// Domains returns the canonical schemes this host serves, sorted.
func (h *Host) Domains() []string {
	out := make([]string, 0, len(h.mounts))
	for s := range h.mounts {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Domain returns the descriptor of a mounted domain by scheme or alias.
func (h *Host) Domain(scheme string) (DomainInfo, bool) {
	m, ok := h.mounts[canonScheme(scheme)]
	if !ok {
		return DomainInfo{}, false
	}
	return m.info, true
}

// Resolve turns any input into a canonical URI. It accepts a string that is
// already a resource URI (canonicalizing its scheme), or an https URL whose host
// a mounted domain claims (handing it to that domain's Classify). A bare id or
// @handle is ambiguous without a scheme; use ResolveOn for that.
func (h *Host) Resolve(input string) (URI, error) {
	input = strings.TrimSpace(input)
	if u, err := ParseURI(input); err == nil {
		return h.canon(u)
	}
	if scheme, ok := h.schemeForURL(input); ok {
		return h.ResolveOn(scheme, input)
	}
	return URI{}, errs.Usage("cannot resolve %q: not a resource URI or a known site URL; pass a scheme with --on", input)
}

// ResolveOn turns an input into a URI within a named domain, the home of bare
// ids and @handles. It reuses the domain's own Classify parser, so it accepts
// every form that domain's CLI accepts.
func (h *Host) ResolveOn(scheme, input string) (URI, error) {
	m, ok := h.mounts[canonScheme(scheme)]
	if !ok {
		return URI{}, errs.Usage("unknown domain: %q", scheme)
	}
	res, ok := m.dom.(Resolver)
	if !ok {
		// No resolver: the input must already be URI-shaped for this scheme.
		if u, err := ParseURI(input); err == nil {
			return h.canon(u)
		}
		return URI{}, errs.Usage("domain %q cannot classify %q", scheme, input)
	}
	typ, id, err := res.Classify(input)
	if err != nil {
		return URI{}, err
	}
	return URI{Scheme: canonScheme(scheme), Authority: strings.ToLower(typ), Path: splitID(id)}, nil
}

// Locate returns the live https location of a URI, the inverse of Resolve. It
// asks the owning domain's Resolver; a domain without one cannot be located.
func (h *Host) Locate(u URI) (string, error) {
	m, ok := h.mounts[canonScheme(u.Scheme)]
	if !ok {
		return "", errs.Usage("unknown domain: %q", u.Scheme)
	}
	res, ok := m.dom.(Resolver)
	if !ok {
		return "", errs.Usage("domain %q cannot locate URLs", u.Scheme)
	}
	return res.Locate(u.Authority, u.ID())
}

// Get dereferences a URI to its record by routing to the domain op that mints
// that authority's type and passing the id as the op's argument. It returns the
// single record the op emits.
func (h *Host) Get(ctx context.Context, u URI) (any, error) {
	m, ok := h.mounts[canonScheme(u.Scheme)]
	if !ok {
		return nil, errs.Usage("unknown domain: %q", u.Scheme)
	}
	op, ok := m.resolvers[u.Authority]
	if !ok {
		return nil, errs.Usage("%s has no resource type %q", u.Scheme, u.Authority)
	}
	recs, err := m.invoke(ctx, op, u.ID(), 1)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, errs.NoResults("no record for %s", u.String())
	}
	return recs[0], nil
}

// List returns the member records of a collection URI by routing to the domain's
// list op for that authority and passing the parent id. limit caps the result
// (0 means the op's own default).
func (h *Host) List(ctx context.Context, u URI, limit int) ([]any, error) {
	m, ok := h.mounts[canonScheme(u.Scheme)]
	if !ok {
		return nil, errs.Usage("unknown domain: %q", u.Scheme)
	}
	op, ok := m.lists[u.Authority]
	if !ok {
		return nil, errs.Usage("%s has no list for %q", u.Scheme, u.Authority)
	}
	return m.invoke(ctx, op, u.ID(), limit)
}

// Searchable reports whether a domain (by scheme or alias) registered a
// free-text search op, so a host such as ant can decide to offer a search box.
func (h *Host) Searchable(scheme string) bool {
	m, ok := h.mounts[canonScheme(scheme)]
	return ok && m.search != nil
}

// Search runs a domain's free-text search op for a query and returns the records
// it emits, each still the domain's own type (so a caller can Wrap or Mint the
// hits that are URI-addressable). limit caps the result (0 means the op's own
// default). It is the query counterpart to Get and List: where those dereference
// a name, Search discovers names from text.
func (h *Host) Search(ctx context.Context, scheme, query string, limit int) ([]any, error) {
	m, ok := h.mounts[canonScheme(scheme)]
	if !ok {
		return nil, errs.Usage("unknown domain: %q", scheme)
	}
	if m.search == nil {
		return nil, errs.Unsupported("domain %q has no search", scheme)
	}
	return m.invoke(ctx, m.search, query, limit)
}

// Links returns the outbound graph edges of a record: one URI per kit:"link"
// field value, with multi-valued and optional fields handled. It is pure
// reflection over the record's tags, so it needs no network and no host lookup
// beyond canonicalizing each link's scheme.
func (h *Host) Links(rec any) []URI {
	t := derefType(reflect.TypeOf(rec))
	if t == nil {
		return nil
	}
	var out []URI
	for _, lf := range linkFields(t) {
		for _, id := range fieldStrings(rec, lf.index) {
			out = append(out, URI{
				Scheme:    canonScheme(lf.scheme),
				Authority: lf.authority,
				Path:      splitID(id),
			})
		}
	}
	return out
}

// Mint returns the URI that names a record, derived from the type the record was
// emitted as (which fixes its scheme and authority) and its id field. It is the
// inverse of Get and the canonical name a data export writes under @id.
func (h *Host) Mint(rec any) (URI, error) {
	t := derefType(reflect.TypeOf(rec))
	if t == nil {
		return URI{}, errs.Usage("cannot mint a URI for a non-struct value")
	}
	tp, ok := h.mint[t]
	if !ok {
		return URI{}, errs.Usage("no domain mints %s records", t.Name())
	}
	ids := fieldStrings(rec, tp.idIdx)
	if len(ids) == 0 {
		return URI{}, errs.Usage("%s record has no id", t.Name())
	}
	return URI{Scheme: tp.scheme, Authority: tp.authority, Path: splitID(ids[0])}, nil
}

// Body returns the record's long-text body (the kit:"body" field) and whether
// it has one, so `ant cat` and the Markdown export can print the human-readable
// text without knowing which field holds it. It is pure reflection over the tag.
func (h *Host) Body(rec any) (string, bool) {
	t := derefType(reflect.TypeOf(rec))
	idx := bodyFieldIndex(t)
	if idx == nil {
		return "", false
	}
	ss := fieldStrings(rec, idx)
	if len(ss) == 0 {
		return "", false
	}
	return ss[0], true
}

// canon resolves a parsed URI's scheme to its canonical form and checks the
// domain (or reserved kind) is one this host knows.
func (h *Host) canon(u URI) (URI, error) {
	if IsReservedKind(u.Scheme) {
		return u, nil
	}
	canon := canonScheme(u.Scheme)
	if _, ok := h.mounts[canon]; !ok {
		return URI{}, errs.Usage("no domain for scheme %q", u.Scheme)
	}
	u.Scheme = canon
	return u, nil
}

// schemeForURL returns the scheme of the domain that claims a pasted https URL's
// host, matching on a Hosts suffix so "www.goodreads.com" matches "goodreads.com".
func (h *Host) schemeForURL(input string) (string, bool) {
	host := urlHost(input)
	if host == "" {
		return "", false
	}
	for scheme, m := range h.mounts {
		for _, owned := range m.info.Hosts {
			owned = strings.ToLower(owned)
			if host == owned || strings.HasSuffix(host, "."+owned) {
				return scheme, true
			}
		}
	}
	return "", false
}

// invoke runs one op with a single positional argument and collects the records
// it emits, building the domain client on first use.
func (m *mount) invoke(ctx context.Context, op Operation, arg string, limit int) ([]any, error) {
	client, err := m.clientFor(ctx)
	if err != nil {
		return nil, err
	}
	c := &collector{}
	in := Input{Args: []string{arg}, Globals: Globals{Limit: limit}}
	rt := RunContext{Client: client, Store: m.store, Limit: limit}
	if err := op.Invoke(ctx, in, rt, c); err != nil {
		if errs.KindOf(err) == errs.KindNoResults {
			return nil, nil
		}
		return nil, err
	}
	return c.recs, nil
}

// clientFor builds and caches this mount's domain client, the same factory the
// single-site binary uses, fed the host-tuned config.
func (m *mount) clientFor(ctx context.Context) (any, error) {
	m.once.Do(func() {
		if m.app.newCli != nil {
			m.client, m.cerr = m.app.newCli(ctx, m.cfg)
		}
	})
	return m.client, m.cerr
}

// collector is the Sink that gathers a single op's records for a host call.
type collector struct{ recs []any }

func (c *collector) Emit(rec any) error { c.recs = append(c.recs, rec); return nil }
func (c *collector) Flush() error       { return nil }

// urlHost extracts the lower-cased host of an http(s) URL, or "" when input is
// not an absolute URL. It is how a pasted link is matched to the domain that
// owns its host.
func urlHost(input string) string {
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// splitID splits a slash-bearing id into URI path segments, so a composite id
// (archive's web/<ts>/<url>) becomes a multi-segment path while a plain id stays
// one segment.
func splitID(id string) []string {
	if id == "" {
		return nil
	}
	var out []string
	for seg := range strings.SplitSeq(id, "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}
