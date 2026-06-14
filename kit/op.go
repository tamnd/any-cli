package kit

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// OpMeta is the surface-neutral description of one operation. The same metadata
// drives the CLI subcommand, the HTTP route, and the MCP tool.
type OpMeta struct {
	Name    string   // the verb: CLI subcommand, MCP tool suffix, route leaf
	Parent  string   // optional parent command for a nested verb (e.g. "rank" for "rank domain")
	Group   string   // grouping for help (e.g. "read", "data", "meta")
	Summary string   // one line, shown in help, OpenAPI summary, MCP description
	Long    string   // optional multi-line help
	Aliases []string // alternate names
	Args    []Arg    // positional argument schema (names line up with In arg fields)
	Write   bool     // a state-changing op; gated and annotated on every surface
	Single  bool     // emits at most one record (no "no results" on empty stream)

	// URI metadata (8000_uri, 8000_uri_drivers). These make an op addressable by
	// resource URI in a multi-domain host such as ant. They are inert on a
	// single-site binary, so a domain can adopt them with no behavior change.
	URIType  string // the URI authority this op dereferences, e.g. "status" for x's tweet; defaults to the lower-cased Out type name
	Resolver bool   // canonical dereferencer for URIType when several ops share it
	List     bool   // member-lister for a parent resource: `ant ls` / feed:// resolve here
}

// key is the space-joined command path, "rank domain" for a nested op or just
// "search" for a top-level one. It is the unique registry key.
func (m OpMeta) key() string {
	if m.Parent != "" {
		return m.Parent + " " + m.Name
	}
	return m.Name
}

// toolName is the MCP tool / OpenAPI operationId form of the command path, with
// the separator collapsed to an underscore: "rank_domain".
func (m OpMeta) toolName() string {
	if m.Parent != "" {
		return m.Parent + "_" + m.Name
	}
	return m.Name
}

// routePath is the HTTP route form of the command path: "rank/domain".
func (m OpMeta) routePath() string {
	if m.Parent != "" {
		return m.Parent + "/" + m.Name
	}
	return m.Name
}

// Arg describes one positional argument for help and schema generation.
type Arg struct {
	Name     string
	Help     string
	Optional bool
	Variadic bool
}

// ParamKind classifies how a struct field is filled.
type ParamKind int

const (
	KindArg    ParamKind = iota // positional argument
	KindFlag                    // named flag / query param / tool argument
	KindInject                  // filled by kit (client, store, cache)
)

// ParamType is the wire type of a parameter.
type ParamType int

const (
	TypeString ParamType = iota
	TypeInt
	TypeBool
	TypeFloat
	TypeDuration
	TypeStringSlice
)

// ParamSpec is the external description of one bound parameter, used to build
// CLI flags, query params, and JSON Schema properties.
type ParamSpec struct {
	Name     string
	Kind     ParamKind
	Type     ParamType
	Short    string
	Help     string
	Default  string
	Enum     []string
	Variadic bool
	Inherit  bool // bind to a framework-global flag of the same name
}

// Operation is what the registry stores and every surface drives. The concrete
// implementation is the generic op[In, Out] built by Handle.
type Operation interface {
	Meta() OpMeta
	Params() []ParamSpec
	InputSchema() map[string]any
	OutputSchema() map[string]any
	// Invoke binds the surface-neutral Input to the typed In, runs the handler,
	// and routes each emitted record to the sink, applying the store tee and the
	// limit. rt carries the injected handles.
	Invoke(ctx context.Context, in Input, rt RunContext, sink Sink) error
	// OutType is the record type the op emits (struct, pointer stripped), or nil
	// when the op emits no struct. A multi-domain host indexes ops by it so a
	// resource URI's authority can select the op that mints that record type.
	OutType() reflect.Type
}

type fieldBind struct {
	index []int
	spec  ParamSpec
}

type op[In, Out any] struct {
	meta   OpMeta
	fn     func(context.Context, In, func(Out) error) error
	binds  []fieldBind
	args   []fieldBind // arg fields in positional order
	outTyp reflect.Type
	outCol string
	idIdx  []int // index path to the kit:"id" field of Out (nil if none)
}

// Handle registers a typed handler. It is the only registration call a domain
// makes. It reflects In to build the parameter set once, and Out to learn the
// record's collection name and primary key for the store tee.
func Handle[In, Out any](app *App, meta OpMeta, fn func(context.Context, In, func(Out) error) error) {
	o := &op[In, Out]{meta: meta, fn: fn}
	o.reflectIn()
	o.reflectOut()
	app.register(o)
}

func (o *op[In, Out]) Meta() OpMeta { return o.meta }

func (o *op[In, Out]) OutType() reflect.Type { return o.outTyp }

func (o *op[In, Out]) Params() []ParamSpec {
	out := make([]ParamSpec, 0, len(o.binds))
	for _, b := range o.binds {
		if b.spec.Kind != KindInject {
			out = append(out, b.spec)
		}
	}
	return out
}

func (o *op[In, Out]) reflectIn() {
	var zero In
	t := reflect.TypeOf(zero)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return
	}
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("kit")
		if !ok {
			continue
		}
		b := parseBind(f, tag)
		o.binds = append(o.binds, b)
		if b.spec.Kind == KindArg {
			o.args = append(o.args, b)
		}
	}
}

func (o *op[In, Out]) reflectOut() {
	var zero Out
	t := reflect.TypeOf(zero)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	o.outTyp = t
	if t == nil || t.Kind() != reflect.Struct {
		o.outCol = "records"
		return
	}
	o.outCol = t.Name()
	o.idIdx = idFieldIndex(t)
}

func parseBind(f reflect.StructField, tag string) fieldBind {
	parts := strings.Split(tag, ",")
	head := strings.TrimSpace(parts[0])
	spec := ParamSpec{
		Name: snake(f.Name),
		Type: paramType(f.Type),
		Help: f.Tag.Get("help"),
	}
	switch head {
	case "arg":
		spec.Kind = KindArg
	case "inject":
		spec.Kind = KindInject
	default: // "flag" or empty
		spec.Kind = KindFlag
	}
	for _, opt := range parts[1:] {
		opt = strings.TrimSpace(opt)
		switch {
		case opt == "variadic":
			spec.Variadic = true
		case opt == "inherit":
			spec.Inherit = true
		case strings.HasPrefix(opt, "name="):
			spec.Name = strings.TrimPrefix(opt, "name=")
		case strings.HasPrefix(opt, "short="):
			spec.Short = strings.TrimPrefix(opt, "short=")
		}
	}
	if d, ok := f.Tag.Lookup("default"); ok {
		spec.Default = d
	}
	if e, ok := f.Tag.Lookup("enum"); ok {
		for v := range strings.SplitSeq(e, ",") {
			if v = strings.TrimSpace(v); v != "" {
				spec.Enum = append(spec.Enum, v)
			}
		}
	}
	return fieldBind{index: f.Index, spec: spec}
}

var durationType = reflect.TypeFor[time.Duration]()

func paramType(t reflect.Type) ParamType {
	if t == durationType {
		return TypeDuration
	}
	switch t.Kind() {
	case reflect.String:
		return TypeString
	case reflect.Bool:
		return TypeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return TypeInt
	case reflect.Float32, reflect.Float64:
		return TypeFloat
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return TypeStringSlice
		}
	}
	return TypeString
}

// Invoke is the one place the limit, the store tee, and no-results detection
// live, so every op gets them without a per-handler stream loop.
func (o *op[In, Out]) Invoke(ctx context.Context, in Input, rt RunContext, sink Sink) error {
	var inv In
	if err := o.bind(&inv, in, rt); err != nil {
		return errs.Usage("%v", err)
	}
	tee := newTee(ctx, rt.Store, o.outCol, o.idIdx)
	n := 0
	emit := func(rec Out) error {
		tee(rec)
		if err := sink.Emit(rec); err != nil {
			return err
		}
		n++
		if rt.Limit > 0 && n >= rt.Limit {
			return errStop
		}
		return nil
	}
	err := o.fn(ctx, inv, emit)
	if ferr := sink.Flush(); ferr != nil && err == nil {
		err = ferr
	}
	if err != nil && err != errStop {
		return err
	}
	if !o.meta.Single && n == 0 {
		return errs.NoResults("no results")
	}
	return nil
}

func hasOpt(tag, want string) bool {
	for p := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(p) == want {
			return true
		}
	}
	return false
}

func jsonNameOf(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return strings.ToLower(f.Name)
	}
	return name
}

func snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
