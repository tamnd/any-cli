package kit

import (
	"reflect"
	"strconv"
	"strings"
)

// reflectx.go holds the struct-tag reflection the URI layer shares with the
// store and the renderer: where a record's primary key, its body, and its
// outbound links live. It reads the same `kit:` tag grammar 8000_uri_drivers §2.3
// defines, so a record that is already kit-correct needs no extra code to be
// URI-addressable.

// idFieldIndex returns the index path of a struct's primary-key field: the field
// tagged kit:"id", or failing that the field whose json name is "id". It returns
// nil when the type has neither.
func idFieldIndex(t reflect.Type) []int {
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var fallback []int
	for f := range t.Fields() {
		if tag, ok := f.Tag.Lookup("kit"); ok && hasOpt(tag, "id") {
			idx := append([]int(nil), f.Index...)
			return idx
		}
		if fallback == nil && jsonNameOf(f) == "id" {
			fallback = append([]int(nil), f.Index...)
		}
	}
	return fallback
}

// bodyFieldIndex returns the index path of a struct's body field: the field
// tagged kit:"body" (the long-text field rendered as Markdown). It returns nil
// when the type has none.
func bodyFieldIndex(t reflect.Type) []int {
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	for f := range t.Fields() {
		if tag, ok := f.Tag.Lookup("kit"); ok && hasHead(tag, "body") {
			return append([]int(nil), f.Index...)
		}
	}
	return nil
}

// linkField is one reference field on a record: where it lives, what kind of
// resource it points at, and whether it is optional or multi-valued.
type linkField struct {
	index     []int
	jsonName  string
	scheme    string // the link target's URI scheme, from kind=<scheme>/<type>
	authority string // the link target's URI authority (record type)
	optional  bool
	slice     bool
}

// linkFields returns every field tagged kit:"link,kind=<scheme>/<type>" on t.
// A slice field yields one URI per element; an optional field yields none when
// blank. The kind is split on the first "/", so "goodreads/author" gives scheme
// "goodreads", authority "author".
func linkFields(t reflect.Type) []linkField {
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var out []linkField
	for f := range t.Fields() {
		tag, ok := f.Tag.Lookup("kit")
		if !ok || !hasHead(tag, "link") {
			continue
		}
		lf := linkField{
			index:    append([]int(nil), f.Index...),
			jsonName: jsonNameOf(f),
			optional: hasOpt(tag, "optional"),
			slice:    f.Type.Kind() == reflect.Slice,
		}
		for p := range strings.SplitSeq(tag, ",") {
			p = strings.TrimSpace(p)
			if kind, ok := strings.CutPrefix(p, "kind="); ok {
				lf.scheme, lf.authority, _ = strings.Cut(kind, "/")
			}
		}
		if lf.scheme == "" || lf.authority == "" {
			continue // a link with no kind is ignored rather than fatal
		}
		out = append(out, lf)
	}
	return out
}

// hasHead reports whether the tag's first comma-separated element equals head,
// e.g. hasHead(`link,kind=x/user`, "link") is true. It distinguishes the tag's
// role ("link", "body", "arg") from its modifiers.
func hasHead(tag, head string) bool {
	first, _, _ := strings.Cut(tag, ",")
	return strings.TrimSpace(first) == head
}

// derefType strips pointers from a type, returning the underlying type and
// whether anything was a struct.
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// derefValue strips pointers from a value, returning the zero Value when a nil
// pointer is met.
func derefValue(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// fieldStrings returns the string form(s) of the field at index on the record:
// one value for a scalar, many for a slice. Empty strings are dropped.
func fieldStrings(rec any, index []int) []string {
	v := derefValue(reflect.ValueOf(rec))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil
	}
	fv := v.FieldByIndex(index)
	if fv.Kind() == reflect.Slice {
		out := make([]string, 0, fv.Len())
		for i := 0; i < fv.Len(); i++ {
			if s := scalarString(fv.Index(i)); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if s := scalarString(fv); s != "" {
		return []string{s}
	}
	return nil
}

// scalarString renders one scalar field value as the string used in a URI id.
func scalarString(fv reflect.Value) string {
	switch fv.Kind() {
	case reflect.String:
		return fv.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(fv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(fv.Uint(), 10)
	case reflect.Pointer:
		return scalarString(derefValue(fv))
	default:
		if !fv.IsValid() {
			return ""
		}
		return ""
	}
}
