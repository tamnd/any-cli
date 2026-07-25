package render

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func renderRecords(t *testing.T, o Options, recs ...Record) string {
	t.Helper()
	var buf bytes.Buffer
	o.Writer = &buf
	r, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, rec := range recs {
		if err := r.Emit(rec); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return buf.String()
}

// captureEncoder is a RowEncoder that records the rows it is handed, so a test
// can assert the registry routed Emit/Flush through it.
type captureEncoder struct {
	rows   [][]string
	closed bool
}

func (e *captureEncoder) EmitRow(cols, vals []string) error {
	e.rows = append(e.rows, vals)
	return nil
}
func (e *captureEncoder) Close() error { e.closed = true; return nil }

func TestRegisteredEncoderRoutesRowsAndCloses(t *testing.T) {
	const fmtName Format = "capture-test"
	enc := &captureEncoder{}
	RegisterEncoder(fmtName, func(w io.Writer, o Options) (RowEncoder, error) { return enc, nil })

	var buf bytes.Buffer
	r, err := New(Options{Format: fmtName, Writer: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Emit(Record{Cols: []string{"url", "status"}, Vals: []string{"https://a/", "200"}}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(enc.rows) != 1 || enc.rows[0][0] != "https://a/" {
		t.Errorf("encoder did not receive the row: %v", enc.rows)
	}
	if !enc.closed {
		t.Errorf("Flush did not close the encoder")
	}
	found := false
	for _, f := range RegisteredFormats() {
		if f == fmtName {
			found = true
		}
	}
	if !found {
		t.Errorf("RegisteredFormats missing %q: %v", fmtName, RegisteredFormats())
	}
}

func TestRecordCSV(t *testing.T) {
	out := renderRecords(t, Options{Format: CSV},
		Record{Cols: []string{"url", "status"}, Vals: []string{"https://a/", "200"}},
		Record{Cols: []string{"url", "status"}, Vals: []string{"https://b/", "404"}},
	)
	if !strings.HasPrefix(out, "url,status\n") {
		t.Errorf("missing header: %q", out)
	}
	if !strings.Contains(out, "https://a/,200") {
		t.Errorf("missing row: %q", out)
	}
}

func TestRecordFieldsProjection(t *testing.T) {
	out := renderRecords(t, Options{Format: CSV, Fields: []string{"status"}},
		Record{Cols: []string{"url", "status"}, Vals: []string{"https://a/", "200"}},
	)
	if strings.Contains(out, "https://a/") {
		t.Errorf("projection leaked url: %q", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("projection dropped status: %q", out)
	}
}

func TestListSectionsMarkdown(t *testing.T) {
	// Color off: a heading from the first column, then literal markdown bullets,
	// with a blank line between records so it streams as valid GFM.
	out := renderRecords(t, Options{Format: List},
		Record{Cols: []string{"username", "name", "followers"}, Vals: []string{"karpathy", "Andrej", "2991308"}},
		Record{Cols: []string{"username", "name", "followers"}, Vals: []string{"nasa", "NASA", "92099694"}},
	)
	want := "## karpathy\n" +
		"- **name**: Andrej\n" +
		"- **followers**: 2991308\n" +
		"\n" +
		"## nasa\n" +
		"- **name**: NASA\n" +
		"- **followers**: 92099694\n"
	if out != want {
		t.Errorf("list = %q, want %q", out, want)
	}
}

func TestListSectionsAlias(t *testing.T) {
	out := renderRecords(t, Options{Format: "sections"},
		Record{Cols: []string{"id", "text"}, Vals: []string{"20", "hi"}},
	)
	if !strings.HasPrefix(out, "## 20\n- **text**: hi\n") {
		t.Errorf("sections alias did not resolve to list: %q", out)
	}
}

func TestListNoHeaderDropsHeading(t *testing.T) {
	// --no-header turns the first column back into a bullet instead of a heading.
	out := renderRecords(t, Options{Format: List, NoHeader: true},
		Record{Cols: []string{"username", "name"}, Vals: []string{"karpathy", "Andrej"}},
	)
	want := "- **username**: karpathy\n- **name**: Andrej\n"
	if out != want {
		t.Errorf("list --no-header = %q, want %q", out, want)
	}
}

func TestListColorUsesANSINotMarkers(t *testing.T) {
	// On a terminal the markdown markers give way to ANSI styling, so the clean
	// view carries no literal ## or ** and does carry color escapes.
	out := renderRecords(t, Options{Format: List, Color: true},
		Record{Cols: []string{"username", "name"}, Vals: []string{"karpathy", "Andrej"}},
	)
	if strings.Contains(out, "##") || strings.Contains(out, "**") {
		t.Errorf("colored list leaked markdown markers: %q", out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("colored list missing ANSI: %q", out)
	}
	if !strings.Contains(out, "karpathy") || !strings.Contains(out, "Andrej") {
		t.Errorf("colored list dropped content: %q", out)
	}
}

func TestRecordJSONUsesValue(t *testing.T) {
	// The table columns differ from the JSON shape: json must marshal Value.
	out := renderRecords(t, Options{Format: JSONL},
		Record{
			Cols:  []string{"status"},
			Vals:  []string{"200"},
			Value: map[string]any{"status": 200, "extra": "kept"},
		},
	)
	if !strings.Contains(out, `"extra":"kept"`) || !strings.Contains(out, `"status":200`) {
		t.Errorf("jsonl did not marshal Value: %q", out)
	}
}

func TestRecordJSONNilValueFallsBackToColumns(t *testing.T) {
	out := renderRecords(t, Options{Format: JSONL},
		Record{Cols: []string{"a", "b"}, Vals: []string{"1", "2"}},
	)
	if !strings.Contains(out, `"a":"1"`) || !strings.Contains(out, `"b":"2"`) {
		t.Errorf("nil Value did not fall back to columns: %q", out)
	}
}

func TestRecordURL(t *testing.T) {
	out := renderRecords(t, Options{Format: URL},
		Record{Cols: []string{"timestamp", "url"}, Vals: []string{"2026", "https://a/"}},
	)
	if strings.TrimSpace(out) != "https://a/" {
		t.Errorf("url format = %q", out)
	}
}

func TestRendererWriteRaw(t *testing.T) {
	var buf bytes.Buffer
	r, err := New(Options{Format: JSONL, Writer: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.Write([]byte("raw bytes")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "raw bytes" {
		t.Errorf("Write = %q", buf.String())
	}
}

func TestTablePlainHasNoANSI(t *testing.T) {
	out := renderRecords(t, Options{Format: Table, Color: false},
		Record{Cols: []string{"a", "b"}, Vals: []string{"hi", "1"}},
	)
	if strings.Contains(out, "\x1b") {
		t.Errorf("plain table leaked ANSI: %q", out)
	}
	if !strings.Contains(out, "╭") || !strings.Contains(out, "A") || !strings.Contains(out, "hi") {
		t.Errorf("table missing border/header/value: %q", out)
	}
}

func TestTableColorEmitsANSI(t *testing.T) {
	out := renderRecords(t, Options{Format: Table, Color: true},
		Record{Cols: []string{"a"}, Vals: []string{"hi"}},
	)
	if !strings.Contains(out, "\x1b") {
		t.Errorf("colored table emitted no ANSI: %q", out)
	}
}

func TestMarkdownTable(t *testing.T) {
	out := renderRecords(t, Options{Format: Markdown},
		Record{Cols: []string{"a", "b"}, Vals: []string{"hi", "1"}},
		Record{Cols: []string{"a", "b"}, Vals: []string{"yo", "2"}},
	)
	if strings.Contains(out, "\x1b") {
		t.Errorf("markdown must never be colored: %q", out)
	}
	// A valid GitHub pipe table: header keeps its case, a dashed rule follows,
	// and rows are piped.
	if !strings.Contains(out, "| a ") || !strings.Contains(out, "|---") || !strings.Contains(out, "| hi ") {
		t.Errorf("not a markdown table: %q", out)
	}
}

func TestMarkdownAlias(t *testing.T) {
	r, err := New(Options{Format: "md", Writer: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Format() != Markdown {
		t.Errorf("md did not resolve to markdown: %q", r.Format())
	}
}

func TestJSONLColorGating(t *testing.T) {
	plain := renderRecords(t, Options{Format: JSONL, Color: false},
		Record{Cols: []string{"n"}, Vals: []string{"1"}, Value: map[string]any{"n": 1, "ok": true, "s": "x"}},
	)
	if strings.Contains(plain, "\x1b") {
		t.Errorf("piped jsonl leaked ANSI: %q", plain)
	}
	colored := renderRecords(t, Options{Format: JSONL, Color: true},
		Record{Cols: []string{"n"}, Vals: []string{"1"}, Value: map[string]any{"n": 1, "ok": true, "s": "x"}},
	)
	if !strings.Contains(colored, "\x1b") {
		t.Errorf("colored jsonl emitted no ANSI: %q", colored)
	}
	// The bytes, once ANSI is stripped, must still be the same JSON.
	if stripANSI(colored) != plain {
		t.Errorf("color changed the JSON bytes:\n plain=%q\n strip=%q", plain, stripANSI(colored))
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip the 'm'
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestEmptyGridEmitsNothing(t *testing.T) {
	var buf bytes.Buffer
	r, _ := New(Options{Format: Table, Writer: &buf})
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("empty table wrote %q", buf.String())
	}
}

func TestTableShrinksToWidth(t *testing.T) {
	wide := strings.Repeat("x", 80)
	out := renderRecords(t, Options{Format: Table, Width: 40},
		Record{Cols: []string{"a", "b"}, Vals: []string{wide, "1"}},
	)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line exceeds width 40 (%d): %q", w, line)
		}
	}
}

func TestNarrowTableNotStretched(t *testing.T) {
	out := renderRecords(t, Options{Format: Table, Width: 120},
		Record{Cols: []string{"a", "b"}, Vals: []string{"x", "1"}},
	)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(line); w > 20 {
			t.Errorf("narrow table got stretched to %d: %q", w, line)
		}
	}
}

func TestMarkdownEscapesPipes(t *testing.T) {
	out := renderRecords(t, Options{Format: Markdown},
		Record{Cols: []string{"title", "n"}, Vals: []string{"Beginners | Full Course", "1"}},
	)
	if !strings.Contains(out, `Beginners \| Full Course`) {
		t.Errorf("pipe not escaped in markdown cell: %q", out)
	}
	// Every body/header line must have the same number of unescaped pipes, or a
	// renderer would misalign the columns.
	var want int
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		got := countCellSeparators(line)
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("line %d has %d separators, want %d: %q", i, got, want, line)
		}
	}
}

// countCellSeparators counts unescaped pipes, the real GFM column separators.
func countCellSeparators(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			n++
		}
	}
	return n
}

// base is a record type that other record types embed, the way a family of
// records in one tool shares an id, a url, and where it came from.
type base struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty" table:"url,url"`
}

type stamped struct {
	When time.Time `json:"when"`
}

type embeddedRec struct {
	base
	stamped
	Likes int    `json:"likes"`
	Notes *notes `json:"notes,omitempty"`
}

type notes struct {
	Body string `json:"body"`
}

// deepRec reaches its id through a nil-able embedded pointer, which is the case
// that panics if the plan is walked with FieldByIndex.
type deepRec struct {
	*base
	Likes int `json:"likes"`
}

// Base is the exported form of the same shared type. Only an exported embedded
// type can be rendered as one cell, since an unexported one cannot be read as a
// value.
type Base struct {
	ID string `json:"id"`
}

// namedEmbed keeps a name of its own, so it stays one cell instead of being
// spread into its parts.
type namedEmbed struct {
	Base  `json:"base"`
	Likes int `json:"likes"`
}

func renderValues(t *testing.T, o Options, recs ...any) string {
	t.Helper()
	var buf bytes.Buffer
	o.Writer = &buf
	r, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, rec := range recs {
		if err := r.Emit(rec); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return buf.String()
}

func TestEmbeddedStructsFlattenIntoColumns(t *testing.T) {
	out := renderValues(t, Options{Format: CSV}, &embeddedRec{
		base:    base{ID: "a/b", URL: "https://example.test/a/b"},
		stamped: stamped{When: time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)},
		Likes:   7,
	})
	header, _, _ := strings.Cut(out, "\n")
	if header != "id,url,when,likes,notes" {
		t.Fatalf("header = %q, want the embedded fields promoted", header)
	}
	if !strings.Contains(out, "a/b,https://example.test/a/b") {
		t.Errorf("embedded values missing from row: %q", out)
	}
}

func TestEmbeddedURLTagSurvivesPromotion(t *testing.T) {
	out := renderValues(t, Options{Format: URL}, &embeddedRec{
		base: base{ID: "a/b", URL: "https://example.test/a/b"},
	})
	if strings.TrimSpace(out) != "https://example.test/a/b" {
		t.Errorf("url format = %q, want the promoted url column", out)
	}
}

func TestNilEmbeddedPointerRendersEmpty(t *testing.T) {
	out := renderValues(t, Options{Format: CSV}, &deepRec{Likes: 3})
	header, row, _ := strings.Cut(out, "\n")
	if header != "id,url,likes" {
		t.Fatalf("header = %q, want the promoted fields", header)
	}
	if strings.TrimSpace(row) != ",,3" {
		t.Errorf("row = %q, want empty cells for the nil embed", row)
	}
}

func TestNamedEmbeddedStaysOneColumn(t *testing.T) {
	out := renderValues(t, Options{Format: CSV}, &namedEmbed{Likes: 1})
	header, _, _ := strings.Cut(out, "\n")
	if header != "base,likes" {
		t.Errorf("header = %q, want the named embed kept whole", header)
	}
}

func TestUnexportedEmbeddedStillPromotes(t *testing.T) {
	// encoding/json promotes the exported fields of an unexported embedded
	// struct, and the table follows it, because the alternative is dropping
	// fields that show up in the json output.
	out := renderValues(t, Options{Format: CSV}, &embeddedRec{base: base{ID: "a/b"}})
	if !strings.HasPrefix(out, "id,url,when,likes,notes\n") {
		t.Errorf("unexported embed not promoted: %q", out)
	}
}

func TestTimeEmbeddedIsAValueNotColumns(t *testing.T) {
	type withTime struct {
		time.Time
		Likes int `json:"likes"`
	}
	out := renderValues(t, Options{Format: CSV}, &withTime{Likes: 2})
	header, _, _ := strings.Cut(out, "\n")
	if header != "time,likes" {
		t.Errorf("header = %q, want time kept as one cell", header)
	}
}

// hiddenURL keeps its address out of the table, because the id already says
// where the record lives, and still answers the url format.
type hiddenURL struct {
	ID  string `json:"id"`
	URL string `json:"url" table:"-,url"`
}

func TestHiddenURLColumnStillAnswersURLFormat(t *testing.T) {
	rec := &hiddenURL{ID: "a/b", URL: "https://example.test/a/b"}
	if out := renderValues(t, Options{Format: CSV}, rec); !strings.HasPrefix(out, "id\n") {
		t.Errorf("header = %q, want the url hidden", out)
	}
	if out := renderValues(t, Options{Format: URL}, rec); strings.TrimSpace(out) != rec.URL {
		t.Errorf("url format = %q, want %q", out, rec.URL)
	}
}
