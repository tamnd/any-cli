package kit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/store"
)

// Input is the surface-neutral request a surface hands to Operation.Invoke. The
// CLI fills Args from positional tokens and Flags from parsed flags; the API
// fills them from the path and query; MCP fills Flags from the tool arguments
// object. Globals carries the resolved framework-global flags.
type Input struct {
	Args    []string       // positional arguments in order
	Flags   map[string]any // named parameters, already typed where possible
	Globals Globals        // resolved framework globals
}

// Globals holds the framework-wide flags shared by every operation. They are
// parsed once by the surface and threaded through to Invoke.
type Globals struct {
	Limit int // --limit / -n; 0 means no limit
}

// RunContext carries the injected handles an operation may receive: the domain
// client, the optional record store, and the resolved limit. A handler reaches
// these only through KindInject fields, never directly.
type RunContext struct {
	Client any
	Store  store.Store
	Limit  int
}

// bind fills the typed In value from the Input and the RunContext. Arg fields
// take positional tokens in declaration order (a variadic arg takes the rest);
// flag fields take typed values from Flags or fall back to their default; inject
// fields receive the client or store by assignability.
func (o *op[In, Out]) bind(inv *In, in Input, rt RunContext) error {
	v := reflect.ValueOf(inv).Elem()
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	argi := 0
	for _, b := range o.binds {
		fv := v.FieldByIndex(b.index)
		switch b.spec.Kind {
		case KindArg:
			if b.spec.Variadic {
				rest := in.Args[min(argi, len(in.Args)):]
				if err := setSlice(fv, rest); err != nil {
					return fmt.Errorf("argument %s: %w", b.spec.Name, err)
				}
				argi = len(in.Args)
				continue
			}
			if argi < len(in.Args) {
				if err := setScalar(fv, in.Args[argi]); err != nil {
					return fmt.Errorf("argument %s: %w", b.spec.Name, err)
				}
			}
			argi++
		case KindFlag:
			if b.spec.Inherit {
				// Inherited flags carry a resolved framework global rather than a
				// per-command flag. The limit is the only global a handler reads today.
				if b.spec.Name == "limit" {
					if err := setFromString(fv, b.spec.Type, strconv.Itoa(in.Globals.Limit)); err != nil {
						return fmt.Errorf("flag %s: %w", b.spec.Name, err)
					}
				}
				continue
			}
			raw, ok := in.Flags[b.spec.Name]
			if !ok {
				if b.spec.Default == "" {
					continue
				}
				if err := setFromString(fv, b.spec.Type, b.spec.Default); err != nil {
					return fmt.Errorf("flag %s default: %w", b.spec.Name, err)
				}
				continue
			}
			if err := setAny(fv, b.spec.Type, raw); err != nil {
				return fmt.Errorf("flag %s: %w", b.spec.Name, err)
			}
		case KindInject:
			inject(fv, rt)
		}
	}
	return nil
}

// inject fills one KindInject field from the available handles by assignability,
// so a handler can declare `Client *foo.Client `kit:"inject"“ or
// `St store.Store `kit:"inject"“ and receive the right one.
func inject(fv reflect.Value, rt RunContext) {
	if rt.Client != nil {
		cv := reflect.ValueOf(rt.Client)
		if cv.Type().AssignableTo(fv.Type()) {
			fv.Set(cv)
			return
		}
	}
	if rt.Store != nil {
		sv := reflect.ValueOf(rt.Store)
		if sv.Type().AssignableTo(fv.Type()) {
			fv.Set(sv)
		}
	}
}

func setAny(fv reflect.Value, pt ParamType, raw any) error {
	switch pt {
	case TypeStringSlice:
		switch s := raw.(type) {
		case []string:
			return setSlice(fv, s)
		case []any:
			ss := make([]string, len(s))
			for i, e := range s {
				ss[i] = fmt.Sprint(e)
			}
			return setSlice(fv, ss)
		case string:
			return setSlice(fv, splitList(s))
		}
	}
	if s, ok := raw.(string); ok {
		return setFromString(fv, pt, s)
	}
	// Numbers and bools may arrive already typed (from JSON tool arguments).
	rv := reflect.ValueOf(raw)
	if rv.Type().ConvertibleTo(fv.Type()) {
		fv.Set(rv.Convert(fv.Type()))
		return nil
	}
	return setFromString(fv, pt, fmt.Sprint(raw))
}

func setScalar(fv reflect.Value, s string) error {
	return setFromString(fv, paramType(fv.Type()), s)
}

func setFromString(fv reflect.Value, pt ParamType, s string) error {
	switch pt {
	case TypeString:
		fv.SetString(s)
	case TypeBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case TypeInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case TypeFloat:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case TypeDuration:
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		fv.SetInt(int64(d))
	case TypeStringSlice:
		return setSlice(fv, splitList(s))
	default:
		fv.SetString(s)
	}
	return nil
}

func setSlice(fv reflect.Value, vals []string) error {
	if fv.Kind() != reflect.Slice {
		if len(vals) > 0 {
			return setScalar(fv, vals[0])
		}
		return nil
	}
	out := reflect.MakeSlice(fv.Type(), len(vals), len(vals))
	for i, s := range vals {
		if err := setScalar(out.Index(i), s); err != nil {
			return err
		}
	}
	fv.Set(out)
	return nil
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// newTee returns a function that mirrors each emitted record into the store
// before it is rendered, so a read with --db doubles as a crawl. With no store
// it returns a no-op. The record is marshalled once to JSON and its primary key
// extracted from the kit:"id" field (idIdx); an empty key lets the backend
// assign one.
func newTee(ctx context.Context, st store.Store, typeName string, idIdx []int) func(any) {
	if st == nil {
		return func(any) {}
	}
	col := store.Collection(typeName)
	return func(rec any) {
		data, err := json.Marshal(rec)
		if err != nil {
			return
		}
		id := extractID(rec, idIdx)
		_ = st.Upsert(ctx, col, id, data)
	}
}

func extractID(rec any, idIdx []int) string {
	if idIdx == nil {
		return ""
	}
	v := reflect.ValueOf(rec)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	fv := v.FieldByIndex(idIdx)
	switch fv.Kind() {
	case reflect.String:
		return fv.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(fv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(fv.Uint(), 10)
	default:
		return fmt.Sprint(fv.Interface())
	}
}
