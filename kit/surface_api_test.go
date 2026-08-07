package kit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// surface_api_test.go covers the one thing the HTTP surface cannot do with a
// path alone: carry an argument that contains a slash. A site-shaped API is
// full of them, owner/name being the obvious one, and GET /v1/blob/owner/name/a/b.go
// has no reading that recovers where the reference ends and the path begins.

type blobIn struct {
	Ref  string `kit:"arg" help:"owner/name"`
	Path string `kit:"arg" help:"a path inside it"`
}

type blobOut struct {
	ID   string `json:"id" kit:"id"`
	Ref  string `json:"ref"`
	Path string `json:"path"`
}

type queryIn struct {
	Terms []string `kit:"arg,variadic" help:"what to look for"`
}

func newAPITestApp() *App {
	app := New(Identity{Binary: "demo", Short: "demo cli", Version: "0.0.1"})
	Handle(app, OpMeta{
		Name:    "blob",
		Group:   "read",
		Summary: "read one blob",
		Args:    []Arg{{Name: "ref"}, {Name: "path", Optional: true}},
	}, func(_ context.Context, in blobIn, emit func(blobOut) error) error {
		return emit(blobOut{ID: in.Ref + "/" + in.Path, Ref: in.Ref, Path: in.Path})
	})
	Handle(app, OpMeta{
		Name:    "search",
		Group:   "read",
		Summary: "search for something",
		Args:    []Arg{{Name: "terms", Variadic: true}},
	}, func(_ context.Context, in queryIn, emit func(blobOut) error) error {
		return emit(blobOut{ID: "q", Ref: strings.Join(in.Terms, " "), Path: strconv.Itoa(len(in.Terms))})
	})
	return app
}

func getJSON(t *testing.T, h http.Handler, url string) blobOut {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, body %s", url, rec.Code, rec.Body.String())
	}
	var got blobOut
	line := strings.TrimSpace(rec.Body.String())
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("%s: decode %q: %v", url, line, err)
	}
	return got
}

func TestHTTPArgsFromQuery(t *testing.T) {
	app := newAPITestApp()
	h := app.httpServer(&State{}, false)

	// The case a path cannot express. Both arguments contain slashes, and the
	// split point between them is not recoverable from the path.
	got := getJSON(t, h, "/v1/blob?ref=gohugoio/hugo&path=commands/hugo.go")
	if got.Ref != "gohugoio/hugo" || got.Path != "commands/hugo.go" {
		t.Fatalf("named args: %+v", got)
	}

	// The path form still works, and still wins when both are given, so
	// nothing that already worked changes.
	got = getJSON(t, h, "/v1/blob/one/two?ref=ignored")
	if got.Ref != "one" || got.Path != "two" {
		t.Fatalf("positional args: %+v", got)
	}

	// An empty named value is not a value. Otherwise ?ref= would overwrite an
	// argument with the empty string and the failure would read as a missing
	// argument rather than as a typo in the query.
	got = getJSON(t, h, "/v1/blob/one/two?path=")
	if got.Path != "two" {
		t.Fatalf("empty named arg overrode a positional: %+v", got)
	}
}

// TestHTTPVariadicArgFromQuery covers the shape a search op has. There is no
// path to put the query on, so ?terms=... is the whole request, and until it
// bound the handler got an empty list and asked the site for nothing.
func TestHTTPVariadicArgFromQuery(t *testing.T) {
	app := newAPITestApp()
	h := app.httpServer(&State{}, false)

	// One value is one argument, commas and all. A variadic positional holds
	// what the shell would have passed as separate words, and splitting a
	// phrase on its punctuation is not that.
	got := getJSON(t, h, "/v1/search?terms=rick%20astley,%20live")
	if got.Ref != "rick astley, live" || got.Path != "1" {
		t.Fatalf("single named value: %+v", got)
	}

	// Repeat the parameter for more than one.
	got = getJSON(t, h, "/v1/search?terms=rick&terms=astley")
	if got.Ref != "rick astley" || got.Path != "2" {
		t.Fatalf("repeated named value: %+v", got)
	}

	// The path form still works and still wins.
	got = getJSON(t, h, "/v1/search/never/gonna?terms=ignored")
	if got.Ref != "never gonna" {
		t.Fatalf("positional args: %+v", got)
	}

	// And an empty one is no arguments rather than one empty argument.
	got = getJSON(t, h, "/v1/search?terms=")
	if got.Path != "0" {
		t.Fatalf("empty named value became an argument: %+v", got)
	}
}

// TestOpenAPIListsArgsAsQuery keeps the generated spec honest about the above.
// A client generated from a spec that omits ref cannot make the one request
// that works.
func TestOpenAPIListsArgsAsQuery(t *testing.T) {
	app := newAPITestApp()
	spec := app.openAPI()

	paths, _ := spec["paths"].(map[string]any)
	blob, _ := paths["/v1/blob"].(map[string]any)
	get, _ := blob["get"].(map[string]any)
	params, _ := get["parameters"].([]map[string]any)

	seen := map[string]string{}
	for _, p := range params {
		name, _ := p["name"].(string)
		in, _ := p["in"].(string)
		seen[name] = in
	}
	for _, want := range []string{"ref", "path"} {
		if seen[want] != "query" {
			t.Errorf("%s is %q in the spec, want query", want, seen[want])
		}
	}

	// A variadic argument is listed too, as an array. Left out, a search
	// endpoint reads as one that takes no input.
	search, _ := paths["/v1/search"].(map[string]any)
	get, _ = search["get"].(map[string]any)
	params, _ = get["parameters"].([]map[string]any)
	if len(params) != 1 {
		t.Fatalf("search has %d parameters, want 1", len(params))
	}
	if params[0]["name"] != "terms" || params[0]["in"] != "query" {
		t.Fatalf("search parameter: %+v", params[0])
	}
	schema, _ := params[0]["schema"].(map[string]any)
	if schema["type"] != "array" {
		t.Errorf("terms is %q in the spec, want array", schema["type"])
	}
}
