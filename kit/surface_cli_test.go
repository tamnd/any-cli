package kit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// An op marked NoCLI is served over HTTP and MCP and is absent from the command
// line, which is the point: a domain that hand-writes a verb still wants the
// same read reachable by an agent, and a generated subcommand would shadow the
// one it wrote.
func TestNoCLIKeepsTheOpOffTheCommandLine(t *testing.T) {
	app := New(Identity{Binary: "demo", Short: "demo", Version: "0.0.1"})
	Handle(app, OpMeta{
		Name: "hidden", Summary: "served but not typed", NoCLI: true,
		Args: []Arg{{Name: "query", Help: "search text"}},
	}, func(_ context.Context, in searchIn, emit func(repo) error) error {
		return emit(repo{ID: in.Query, Owner: in.Query})
	})
	Handle(app, OpMeta{
		Name: "shown", Summary: "served and typed",
		Args: []Arg{{Name: "query", Help: "search text"}},
	}, func(_ context.Context, in searchIn, emit func(repo) error) error {
		return emit(repo{ID: in.Query, Owner: in.Query})
	})

	for _, c := range app.buildCLI().Commands() {
		if c.Name() == "hidden" {
			t.Error("a NoCLI op reached the command line, where it would shadow the domain's own command")
		}
	}

	var tools []string
	for _, tool := range app.mcpTools() {
		tools = append(tools, tool["name"].(string))
	}
	want := map[string]bool{"hidden": false, "shown": false}
	for _, name := range tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("MCP does not list %s, and NoCLI is about the command line only: %v", name, tools)
		}
	}
}

// A server reading stdin is indistinguishable from a program that has stopped,
// so `mcp` says what it is on stderr before it blocks. On stderr because stdout
// is the JSON-RPC channel, and the count because a server with no tools is worth
// noticing on the first line rather than after a handshake.
func TestMCPSaysItIsListening(t *testing.T) {
	app := newTestApp()
	root := app.buildCLI()
	var errOut, out bytes.Buffer
	root.SetErr(&errOut)
	root.SetOut(&out)
	root.SetArgs([]string{"mcp"})
	root.SetIn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := errOut.String(); !strings.Contains(got, "on stdio") || !strings.Contains(got, "1 tool") {
		t.Errorf("mcp said %q, want a line naming the transport and the tool count", got)
	}
	if got := out.String(); !strings.Contains(got, `"search"`) {
		t.Errorf("tools/list answered %q, want the registered op in it", got)
	}
}

// A word that is not a subcommand is a mistake, and the answer to a mistake
// goes on stderr with a non-zero exit. It used to go on stdout with exit 0, so
// "demo group lst -o jsonl > out.jsonl" left the help text in out.jsonl and
// nothing anywhere said the file was not records.
//
// The root and a group command both get this, and they get the same exit code
// for it, because from where the user sits it is the same mistake.
func TestUnknownSubcommandIsAnError(t *testing.T) {
	app := newTestApp() // has a top-level "search"
	Handle(app, OpMeta{
		Name:    "list",
		Parent:  "group",
		Summary: "list things",
	}, func(_ context.Context, _ struct{}, emit func(repo) error) error {
		return emit(repo{ID: "x"})
	})

	// Each case misspells a command that exists at the level it is typed at, so
	// each one has a suggestion to make. A near-miss of a subcommand is not a
	// near-miss of anything at the root, and cobra is right not to guess there.
	cases := []struct {
		args       []string
		typo, want string
	}{
		{[]string{"group", "lst"}, "lst", "list"},
		{[]string{"serch", "go"}, "serch", "search"},
	}
	for _, c := range cases {
		root := app.buildCLI()
		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(c.args)
		err := root.ExecuteContext(context.Background())
		if err == nil {
			t.Errorf("%v: no error, and the help went to stdout: %q", c.args, out.String())
			continue
		}
		if code := exitCodeFor(err); code != 2 {
			t.Errorf("%v: exit code %d, want 2 for a usage error", c.args, code)
		}
		if want := fmt.Sprintf("unknown command %q", c.typo); !strings.Contains(err.Error(), want) {
			t.Errorf("%v: error is %q, want it to name the word that was wrong", c.args, err)
		}
		// The suggestion is the reason the word is worth naming back.
		if want := fmt.Sprintf("did you mean %q", c.want); !strings.Contains(err.Error(), want) {
			t.Errorf("%v: error is %q, want %q", c.args, err, want)
		}
		if out.Len() > 0 {
			t.Errorf("%v: wrote %q to stdout, which a redirect would have kept", c.args, out.String())
		}
	}
}

// No arguments at all is not a mistake, it is how you ask what is in there, so
// it still prints help on stdout and still exits 0. This is the half of the
// behavior that was right and had to survive making the other half an error.
func TestBareGroupStillPrintsHelp(t *testing.T) {
	app := New(Identity{Binary: "demo", Short: "demo", Version: "0.0.1"})
	Handle(app, OpMeta{
		Name:    "list",
		Parent:  "group",
		Summary: "list things",
	}, func(_ context.Context, _ struct{}, emit func(repo) error) error {
		return emit(repo{ID: "x"})
	})
	// The root lists the group, the group lists the op under it.
	for args, want := range map[string]string{"group": "list things", "": "group commands"} {
		root := app.buildCLI()
		var out, errOut bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(strings.Fields(args))
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Errorf("%q: %v", args, err)
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q: stdout is %q, want the help holding %q", args, out.String(), want)
		}
	}
}

// A group written by hand rather than generated from a parent is the same shape
// and gets the same rule. Six of ccrawl's seventeen group commands are these,
// and they still printed help to stdout after the generated ones stopped.
func TestHandWrittenGroupRejectsUnknownSubcommand(t *testing.T) {
	app := New(Identity{Binary: "demo", Short: "demo", Version: "0.0.1"})
	app.AddCommand(Command{
		Use:   "tool",
		Short: "a hand-written group",
		Sub: []Command{{
			Use:   "show",
			Short: "show it",
			Run:   func(context.Context, []string) error { return nil },
		}},
	})

	root := app.buildCLI()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"tool", "shwo"})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("no error, and the help went to stdout: %q", out.String())
	}
	if code := exitCodeFor(err); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if !strings.Contains(err.Error(), `did you mean "show"`) {
		t.Errorf("error is %q, want the suggestion", err)
	}
	if out.Len() > 0 {
		t.Errorf("wrote %q to stdout", out.String())
	}

	// The subcommand it does have still runs, and the group alone still helps.
	for _, args := range [][]string{{"tool", "show"}, {"tool"}} {
		root := app.buildCLI()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		if err := root.ExecuteContext(context.Background()); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}
