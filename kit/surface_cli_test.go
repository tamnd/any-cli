package kit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what was
// written. invokeCLI renders to os.Stdout directly, so this is how a CLI-surface
// test reads the output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestCLIExecuteJSON(t *testing.T) {
	app := newTestApp()
	root := app.buildCLI()
	root.SetArgs([]string{"search", "go", "--output", "jsonl"})
	out := captureStdout(t, func() {
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Errorf("execute: %v", err)
		}
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 jsonl lines, got %d: %q", len(lines), out)
	}
	var rec repo
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("decode line: %v", err)
	}
	if rec.Owner != "go" {
		t.Fatalf("owner = %q, want go", rec.Owner)
	}
}

func TestCLINestedOp(t *testing.T) {
	app := New(Identity{Binary: "demo", Short: "demo", Version: "0.0.1"})
	Handle(app, OpMeta{
		Name:    "domain",
		Parent:  "rank",
		Summary: "rank a domain",
		Args:    []Arg{{Name: "host"}},
	}, func(_ context.Context, in struct {
		Host string `kit:"arg"`
	}, emit func(repo) error) error {
		return emit(repo{ID: in.Host, Owner: in.Host, Stars: 7})
	})
	app.AddCommandUnder("rank", Command{
		Use:   "info",
		Short: "an escape-hatch sibling",
		Run:   func(context.Context, []string) error { return nil },
	})

	root := app.buildCLI()
	rankCmd, _, err := root.Find([]string{"rank", "domain"})
	if err != nil || rankCmd.Name() != "domain" {
		t.Fatalf("nested op not found: %v (%v)", rankCmd, err)
	}
	infoCmd, _, err := root.Find([]string{"rank", "info"})
	if err != nil || infoCmd.Name() != "info" {
		t.Fatalf("escape-hatch sibling not found: %v (%v)", infoCmd, err)
	}

	root.SetArgs([]string{"rank", "domain", "example.com", "-o", "jsonl"})
	out := captureStdout(t, func() {
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Errorf("execute: %v", err)
		}
	})
	if !strings.Contains(out, "example.com") {
		t.Fatalf("nested op output = %q", out)
	}
}

func TestCLILimitFlag(t *testing.T) {
	app := newTestApp()
	root := app.buildCLI()
	root.SetArgs([]string{"search", "go", "-n", "1", "-o", "jsonl"})
	out := captureStdout(t, func() {
		_ = root.ExecuteContext(context.Background())
	})
	if got := strings.Count(strings.TrimSpace(out), "\n") + 1; got != 1 {
		t.Fatalf("limit 1 should print 1 line, got %d: %q", got, out)
	}
}
