// Package render turns a stream of record structs into one of the output
// formats the kit CLI supports: table, json, jsonl, csv, tsv, url, raw, and a
// Go text/template. It works off struct reflection and the `table:` and `json:`
// tags, so any record type renders without per-type code. It holds no domain
// knowledge and is reusable on its own.
//
// Tag grammar (on a struct field):
//
//	table:"name"            include in the table/csv view under column "name"
//	table:"name,truncate"   truncate long values to the terminal width
//	table:"name,time"       format a time.Time as 2006-01-02 15:04
//	table:"name,url"        mark the canonical URL column (used by the url format)
//	table:"-"               never show in table/csv (still present in json)
//	(no table tag)          fall back to the json tag name
package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"text/template"
	"time"
)

// Format is an output encoding.
type Format string

const (
	Auto     Format = "auto"
	Table    Format = "table"
	JSON     Format = "json"
	JSONL    Format = "jsonl"
	CSV      Format = "csv"
	TSV      Format = "tsv"
	URL      Format = "url"
	Raw      Format = "raw"
	Template Format = "template"
)

// Record is a row a command has already projected: an explicit ordered column
// set (Cols/Vals) for the table, csv, tsv, and url formats, plus the original
// Value rendered by json, jsonl, raw, and template. A command emits a Record when
// its table columns differ from its JSON shape, or when the row comes from a
// dynamic map that struct reflection cannot plan. Emit handles it like any
// record; when Value is nil it falls back to a map of the columns.
type Record struct {
	Cols  []string
	Vals  []string
	Value any
}

// value returns what the json-family formats marshal for this record.
func (rec Record) value() any {
	if rec.Value != nil {
		return rec.Value
	}
	m := make(map[string]any, len(rec.Cols))
	for i, c := range rec.Cols {
		if i < len(rec.Vals) {
			m[c] = rec.Vals[i]
		}
	}
	return m
}

// Options configure a Renderer.
type Options struct {
	Format   Format    // the encoding; Auto resolves to Table on a TTY else JSONL
	IsTTY    bool      // whether the writer is an interactive terminal (for Auto)
	Fields   []string  // projection: restrict/reorder columns by name
	NoHeader bool      // omit the header row in table/csv
	Template string    // when set, format becomes Template
	Width    int       // truncation width for `truncate` columns (0 = no limit)
	Writer   io.Writer // destination
}

// Renderer renders records incrementally; one instance handles a whole run so
// streaming formats write as records arrive.
type Renderer struct {
	o    Options
	w    io.Writer
	tmpl *template.Template

	tw         *tabwriter.Writer
	csvw       *csv.Writer
	headerDone bool
	jsonOpen   bool
	jsonFirst  bool
}

// New builds a Renderer, resolving Auto and compiling any template.
func New(o Options) (*Renderer, error) {
	if o.Writer == nil {
		return nil, fmt.Errorf("render: nil writer")
	}
	r := &Renderer{o: o, w: o.Writer}
	if o.Template != "" {
		t, err := template.New("row").Parse(o.Template + "\n")
		if err != nil {
			return nil, fmt.Errorf("bad --template: %w", err)
		}
		r.tmpl = t
		r.o.Format = Template
	}
	if r.o.Format == "" || r.o.Format == Auto {
		if o.IsTTY {
			r.o.Format = Table
		} else {
			r.o.Format = JSONL
		}
	}
	return r, nil
}

// Format returns the resolved format.
func (r *Renderer) Format() Format { return r.o.Format }

// Emit renders one record. A record is either a struct (rendered by reflection
// and its `table:`/`json:` tags) or a Record with explicit columns.
func (r *Renderer) Emit(rec any) error {
	switch r.o.Format {
	case Table:
		return r.emitTable(rec)
	case CSV, TSV:
		return r.emitDelim(rec)
	case JSONL:
		return r.emitJSONL(jsonValue(rec))
	case JSON:
		return r.emitJSON(jsonValue(rec))
	case URL:
		return r.emitURL(rec)
	case Raw:
		return r.emitRaw(jsonValue(rec))
	case Template:
		return r.tmpl.Execute(r.w, templateData(jsonValue(rec)))
	default:
		return r.emitJSONL(jsonValue(rec))
	}
}

// Write sends raw bytes straight to the underlying writer, bypassing all
// formatting, so a Renderer doubles as the io.Writer for a command that emits an
// opaque blob (a page body, extracted text) alongside any structured rows.
func (r *Renderer) Write(b []byte) (int, error) { return r.w.Write(b) }

// jsonValue is what the json-family formats marshal: a Record's Value, or the
// record itself.
func jsonValue(rec any) any {
	if rd, ok := rec.(Record); ok {
		return rd.value()
	}
	return rec
}

// Flush finalizes buffered formats. Call once at the end of a run.
func (r *Renderer) Flush() error {
	switch {
	case r.tw != nil:
		return r.tw.Flush()
	case r.csvw != nil:
		r.csvw.Flush()
		return r.csvw.Error()
	case r.o.Format == JSON:
		if !r.jsonOpen {
			_, err := io.WriteString(r.w, "[]\n")
			return err
		}
		_, err := io.WriteString(r.w, "\n]\n")
		return err
	}
	return nil
}

func (r *Renderer) columns(rec any) (cols, vals []string) {
	if rd, ok := rec.(Record); ok {
		return r.project(rd.Cols, rd.Vals)
	}
	plan := planFor(reflect.TypeOf(rec))
	rv := reflect.ValueOf(rec)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	for _, c := range plan.cols {
		fv := rv.FieldByIndex(c.index)
		s := stringify(fv, c)
		if c.truncate && r.o.Width > 0 {
			s = truncate(s, r.o.Width)
		}
		cols = append(cols, c.name)
		vals = append(vals, oneline(s))
	}
	return r.project(cols, vals)
}

func (r *Renderer) project(cols, vals []string) ([]string, []string) {
	if len(r.o.Fields) == 0 {
		return cols, vals
	}
	idx := map[string]int{}
	for i, c := range cols {
		idx[c] = i
	}
	pc := make([]string, 0, len(r.o.Fields))
	pv := make([]string, 0, len(r.o.Fields))
	for _, f := range r.o.Fields {
		pc = append(pc, f)
		if i, ok := idx[f]; ok {
			pv = append(pv, vals[i])
		} else {
			pv = append(pv, "")
		}
	}
	return pc, pv
}

func (r *Renderer) emitTable(rec any) error {
	if r.tw == nil {
		r.tw = tabwriter.NewWriter(r.w, 0, 0, 2, ' ', 0)
	}
	cols, vals := r.columns(rec)
	if !r.headerDone && !r.o.NoHeader {
		if _, err := fmt.Fprintln(r.tw, strings.Join(upper(cols), "\t")); err != nil {
			return err
		}
		r.headerDone = true
	}
	_, err := fmt.Fprintln(r.tw, strings.Join(vals, "\t"))
	return err
}

func (r *Renderer) emitDelim(rec any) error {
	if r.csvw == nil {
		r.csvw = csv.NewWriter(r.w)
		if r.o.Format == TSV {
			r.csvw.Comma = '\t'
		}
	}
	cols, vals := r.columns(rec)
	if !r.headerDone && !r.o.NoHeader {
		if err := r.csvw.Write(cols); err != nil {
			return err
		}
		r.headerDone = true
	}
	return r.csvw.Write(vals)
}

func (r *Renderer) emitJSONL(rec any) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.w, string(b))
	return err
}

func (r *Renderer) emitJSON(rec any) error {
	if !r.jsonOpen {
		if _, err := io.WriteString(r.w, "["); err != nil {
			return err
		}
		r.jsonOpen = true
		r.jsonFirst = true
	}
	if !r.jsonFirst {
		if _, err := io.WriteString(r.w, ","); err != nil {
			return err
		}
	}
	r.jsonFirst = false
	b, err := json.MarshalIndent(rec, "  ", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(r.w, "\n  "+string(b))
	return err
}

func (r *Renderer) emitURL(rec any) error {
	if rd, ok := rec.(Record); ok {
		for i, c := range rd.Cols {
			if c == "url" && i < len(rd.Vals) && rd.Vals[i] != "" {
				_, err := fmt.Fprintln(r.w, rd.Vals[i])
				return err
			}
		}
		return nil
	}
	plan := planFor(reflect.TypeOf(rec))
	rv := reflect.ValueOf(rec)
	for rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	// Prefer the field flagged `url`, else a column literally named "url".
	want := plan.urlField
	if want == nil {
		for i := range plan.cols {
			if plan.cols[i].name == "url" {
				want = &plan.cols[i]
				break
			}
		}
	}
	if want == nil {
		return nil
	}
	s := stringify(rv.FieldByIndex(want.index), *want)
	if s == "" {
		return nil
	}
	_, err := fmt.Fprintln(r.w, s)
	return err
}

func (r *Renderer) emitRaw(rec any) error {
	if s, ok := rec.(string); ok {
		_, err := fmt.Fprintln(r.w, s)
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.w, string(b))
	return err
}

// ---- reflection plan (cached per type) ----

type colSpec struct {
	name     string
	index    []int
	truncate bool
	timeFmt  bool
	isURL    bool
}

type typePlan struct {
	cols     []colSpec
	urlField *colSpec
}

var planCache sync.Map // reflect.Type -> *typePlan

func planFor(t reflect.Type) *typePlan {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return &typePlan{}
	}
	if p, ok := planCache.Load(t); ok {
		return p.(*typePlan)
	}
	p := buildPlan(t)
	planCache.Store(t, p)
	return p
}

func buildPlan(t reflect.Type) *typePlan {
	p := &typePlan{}
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, opts, skip := columnName(f)
		if skip {
			continue
		}
		c := colSpec{name: name, index: f.Index}
		for _, o := range opts {
			switch o {
			case "truncate":
				c.truncate = true
			case "time":
				c.timeFmt = true
			case "url":
				c.isURL = true
			}
		}
		p.cols = append(p.cols, c)
	}
	for i := range p.cols {
		if p.cols[i].isURL {
			p.urlField = &p.cols[i]
			break
		}
	}
	return p
}

// columnName resolves a field's table column name and options, falling back to
// the json tag. It returns skip=true for fields hidden from the table view.
func columnName(f reflect.StructField) (name string, opts []string, skip bool) {
	if tag, ok := f.Tag.Lookup("table"); ok {
		parts := strings.Split(tag, ",")
		head := parts[0]
		if head == "-" {
			return "", nil, true
		}
		if head == "" {
			head = jsonName(f)
		}
		return head, parts[1:], false
	}
	jn := jsonName(f)
	if jn == "-" {
		return "", nil, true
	}
	return jn, nil, false
}

func jsonName(f reflect.StructField) string {
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

func stringify(v reflect.Value, c colSpec) string {
	if !v.IsValid() {
		return ""
	}
	if t, ok := v.Interface().(time.Time); ok {
		if t.IsZero() {
			return ""
		}
		if c.timeFmt {
			return t.Format("2006-01-02 15:04")
		}
		return t.Format(time.RFC3339)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return ""
		}
		return stringify(v.Elem(), c)
	case reflect.String:
		return v.String()
	case reflect.Bool:
		if v.Bool() {
			return "true"
		}
		return ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return string(v.Bytes())
		}
		return strconv.Itoa(v.Len()) // a count is the useful table cell for a list
	default:
		b, err := json.Marshal(v.Interface())
		if err != nil {
			return fmt.Sprint(v.Interface())
		}
		return string(b)
	}
}

// templateData makes a template see the same json-tag keys as --json: a struct
// record is round-tripped to a generic map; a map or scalar passes through.
func templateData(rec any) any {
	switch rec.(type) {
	case map[string]any, nil, string:
		return rec
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return rec
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return rec
	}
	return m
}

func truncate(s string, n int) string {
	if n <= 1 || len([]rune(s)) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n-1]) + "…"
}

func oneline(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " ")
}

func upper(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToUpper(s)
	}
	return out
}
