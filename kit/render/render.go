// Package render turns a stream of record structs into one of the output
// formats the kit CLI supports: table, markdown, json, jsonl, csv, tsv, url,
// raw, and a Go text/template. It works off struct reflection and the `table:`
// and `json:` tags, so any record type renders without per-type code. It holds
// no domain knowledge and is reusable on its own.
//
// The table and markdown formats are drawn with lipgloss. table is a
// rounded-border, color-aware grid meant for a terminal; markdown is a
// GitHub-flavored pipe table meant for pasting into docs, issues, and READMEs.
// Both buffer their rows and draw once on Flush so every column sizes to its
// widest cell.
//
// The list format is the readable alternative to a grid: each record becomes a
// short section — a heading drawn from the first column, then the rest as a
// "- **key**: value" bullet list — and records stream as they arrive rather than
// buffering, so a slow command stays responsive. On a terminal (color on) the
// markdown markers give way to ANSI styling for a clean detail view; piped
// (color off) it emits literal GitHub-flavored markdown.
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
)

// Format is an output encoding.
type Format string

const (
	Auto     Format = "auto"
	Table    Format = "table"
	Markdown Format = "markdown"
	List     Format = "list"
	JSON     Format = "json"
	JSONL    Format = "jsonl"
	CSV      Format = "csv"
	TSV      Format = "tsv"
	URL      Format = "url"
	Raw      Format = "raw"
	Template Format = "template"
	Parquet  Format = "parquet"
)

// RowEncoder writes projected rows in a binary or otherwise non-streaming
// format that the core render package does not implement itself. A consumer
// supplies one (for example a Parquet writer) through RegisterEncoder, which
// keeps heavy encoding dependencies out of kit's core.
type RowEncoder interface {
	// EmitRow writes one record's projected columns and values. The column set
	// is stable across a run, so the first call defines the schema.
	EmitRow(cols, vals []string) error
	// Close flushes and finalizes the output. It is called once, from Flush.
	Close() error
}

// EncoderFactory builds a RowEncoder for a destination writer and the run's
// options.
type EncoderFactory func(w io.Writer, o Options) (RowEncoder, error)

var encoderRegistry = map[Format]EncoderFactory{}

// RegisterEncoder teaches kit a format it does not ship, by registering a
// RowEncoder factory for it. A CLI calls this from an init function (for
// example to add "parquet") so the encoding dependency lives in the CLI, not
// in kit. The factory is used only when a run resolves to that exact format.
func RegisterEncoder(f Format, factory EncoderFactory) {
	if factory != nil {
		encoderRegistry[f] = factory
	}
}

// RegisteredFormats returns the formats added through RegisterEncoder, sorted.
// The CLI surface uses it to list only the extra formats a binary actually
// supports in its --output help, rather than advertising every format kit
// could in principle carry.
func RegisteredFormats() []Format {
	out := make([]Format, 0, len(encoderRegistry))
	for f := range encoderRegistry {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// normalizeFormat folds short aliases onto their canonical format.
func normalizeFormat(f Format) Format {
	switch f {
	case "md":
		return Markdown
	case "section", "sections":
		return List
	default:
		return f
	}
}

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
	Color    bool      // emit ANSI color (table/list accents, JSON highlighting); kit resolves --color
	Fields   []string  // projection: restrict/reorder columns by name
	NoHeader bool      // omit the header row in table/csv/markdown, and the heading in list
	Template string    // when set, format becomes Template
	Width    int       // truncation width for `truncate` columns (0 = no limit)
	Writer   io.Writer // destination
}

// Renderer renders records incrementally; one instance handles a whole run so
// streaming formats write as records arrive. The grid formats (table, markdown)
// instead buffer their rows and draw once on Flush.
type Renderer struct {
	o    Options
	w    io.Writer
	tmpl *template.Template

	csvw       *csv.Writer
	headerDone bool
	jsonOpen   bool
	jsonFirst  bool

	gridHead []string   // buffered header for table/markdown
	gridRows [][]string // buffered rows for table/markdown
	gridSeen bool       // whether any grid record has been collected

	listSeen bool // whether a list-format record has already been emitted

	enc RowEncoder // set when the format is served by a registered encoder
}

// New builds a Renderer, resolving Auto and compiling any template.
func New(o Options) (*Renderer, error) {
	if o.Writer == nil {
		return nil, fmt.Errorf("render: nil writer")
	}
	r := &Renderer{o: o, w: o.Writer}
	r.o.Format = normalizeFormat(r.o.Format)
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
	if factory, ok := encoderRegistry[r.o.Format]; ok {
		enc, err := factory(r.w, r.o)
		if err != nil {
			return nil, err
		}
		r.enc = enc
	}
	return r, nil
}

// Format returns the resolved format.
func (r *Renderer) Format() Format { return r.o.Format }

// Emit renders one record. A record is either a struct (rendered by reflection
// and its `table:`/`json:` tags) or a Record with explicit columns.
func (r *Renderer) Emit(rec any) error {
	if r.enc != nil {
		cols, vals := r.columns(rec)
		return r.enc.EmitRow(cols, vals)
	}
	switch r.o.Format {
	case Table, Markdown:
		return r.collectGrid(rec)
	case List:
		return r.emitList(rec)
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
	if r.enc != nil {
		return r.enc.Close()
	}
	switch {
	case r.o.Format == Table || r.o.Format == Markdown:
		return r.flushGrid()
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

// collectGrid buffers one record's columns for the table/markdown formats; the
// grid is drawn once on Flush so every column sizes to its widest cell.
func (r *Renderer) collectGrid(rec any) error {
	cols, vals := r.columns(rec)
	if !r.gridSeen {
		r.gridHead = cols
		r.gridSeen = true
	}
	r.gridRows = append(r.gridRows, vals)
	return nil
}

// emitList renders one record as a readable section: a heading drawn from the
// first column, then the remaining columns as a "- **key**: value" bullet list.
// Records are separated by a blank line. Unlike the table and markdown grids it
// streams — each record prints the moment it arrives — so a slow command stays
// responsive. When color is on (a terminal) the markdown markers give way to
// ANSI styling for a clean detail view; with color off (piped or --color=never)
// it emits literal GitHub-flavored markdown that pastes into an issue or README
// unchanged. --no-header drops the heading, so every column becomes a bullet.
func (r *Renderer) emitList(rec any) error {
	cols, vals := r.columns(rec)
	if len(cols) == 0 {
		return nil
	}
	var b strings.Builder
	if r.listSeen {
		b.WriteByte('\n') // blank line between records
	}
	r.listSeen = true

	bcols, bvals := cols, vals
	if !r.o.NoHeader {
		head := ""
		if len(vals) > 0 {
			head = vals[0]
		}
		if r.o.Color {
			b.WriteString(ansiHead + head + ansiReset + "\n")
		} else {
			b.WriteString("## " + head + "\n")
		}
		bcols, bvals = cols[1:], vals[1:]
	}
	// In the color view, pad keys to the record's widest so values line up into a
	// clean column; the markdown view leaves bullets unpadded so they stay valid.
	keyWidth := 0
	if r.o.Color {
		for _, c := range bcols {
			if n := len([]rune(c)); n > keyWidth {
				keyWidth = n
			}
		}
	}
	for i, c := range bcols {
		v := ""
		if i < len(bvals) {
			v = bvals[i]
		}
		if r.o.Color {
			pad := strings.Repeat(" ", keyWidth-len([]rune(c)))
			b.WriteString("  " + ansiKey + c + ansiReset + pad + "  " + v + "\n")
		} else {
			b.WriteString("- **" + c + "**: " + v + "\n")
		}
	}
	_, err := io.WriteString(r.w, b.String())
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
	_, err = fmt.Fprintln(r.w, r.colorJSON(b))
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
	_, err = fmt.Fprint(r.w, "\n  "+r.colorJSON(b))
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
