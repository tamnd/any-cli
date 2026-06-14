package render

import (
	"bytes"
	"strings"
	"testing"

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
