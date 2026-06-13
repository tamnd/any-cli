package render

import (
	"bytes"
	"strings"
	"testing"
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
