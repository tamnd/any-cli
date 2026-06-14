package kit

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- a fake domain, the smallest thing that exercises the whole driver path ---

type widget struct {
	ID      string `json:"id" kit:"id"`
	Name    string `json:"name"`
	MakerID string `json:"maker_id" kit:"link,kind=demo/maker,optional"`
}

type maker struct {
	ID   string `json:"id" kit:"id"`
	Name string `json:"name"`
}

type part struct {
	ID       string `json:"id" kit:"id"`
	WidgetID string `json:"widget_id"`
}

type idArg struct {
	ID string `kit:"arg"`
}

type queryArg struct {
	Query []string `kit:"arg,variadic"`
}

type demoDomain struct{}

func (demoDomain) Info() DomainInfo {
	return DomainInfo{
		Scheme:   "demo",
		Aliases:  []string{"dm"},
		Hosts:    []string{"demo.example"},
		Identity: Identity{Binary: "demo", Version: "test"},
	}
}

func (demoDomain) Register(app *App) {
	Handle(app, OpMeta{Name: "widget", URIType: "widget", Single: true},
		func(_ context.Context, in idArg, emit func(widget) error) error {
			if in.ID == "" {
				return nil
			}
			return emit(widget{ID: in.ID, Name: "Widget " + in.ID, MakerID: "m1"})
		})
	Handle(app, OpMeta{Name: "maker", URIType: "maker", Single: true},
		func(_ context.Context, in idArg, emit func(maker) error) error {
			return emit(maker{ID: in.ID, Name: "Maker " + in.ID})
		})
	// A list op enumerates a widget's parts: its authority is the parent
	// ("widget"), but it emits a child type that must not seed the mint index.
	Handle(app, OpMeta{Name: "parts", URIType: "widget", List: true},
		func(_ context.Context, in idArg, emit func(part) error) error {
			return emit(part{ID: "p1", WidgetID: in.ID})
		})
	// A free-text search op: not URI-addressable (its URIType reuses "widget"),
	// but a host can still drive it via Host.Search.
	Handle(app, OpMeta{Name: "search", URIType: "widget",
		Args: []Arg{{Name: "query", Variadic: true}}},
		func(_ context.Context, in queryArg, emit func(widget) error) error {
			return emit(widget{ID: "q", Name: "Hit for " + strings.Join(in.Query, " ")})
		})
}

func (demoDomain) Classify(input string) (string, string, error) {
	if rest, ok := strings.CutPrefix(input, "https://demo.example/"); ok {
		typ, id, _ := strings.Cut(rest, "/")
		return typ, id, nil
	}
	return "widget", input, nil // a bare id is a widget
}

func (demoDomain) Locate(typ, id string) (string, error) {
	return "https://demo.example/" + typ + "/" + id, nil
}

func registerDemo(t *testing.T) {
	t.Helper()
	resetDomainsForTest()
	Register(demoDomain{})
	t.Cleanup(resetDomainsForTest)
}

// --- registry ---

func TestRegisterAndLookup(t *testing.T) {
	registerDemo(t)
	if got := Domains(); len(got) != 1 || got[0] != "demo" {
		t.Fatalf("Domains() = %v, want [demo]", got)
	}
	if _, ok := Lookup("demo"); !ok {
		t.Error("Lookup(demo) not found")
	}
	if _, ok := Lookup("dm"); !ok {
		t.Error("Lookup(dm) alias not found")
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) found, want miss")
	}
}

func TestRegisterReservedPanics(t *testing.T) {
	resetDomainsForTest()
	t.Cleanup(resetDomainsForTest)
	defer func() {
		if recover() == nil {
			t.Error("Register with reserved scheme did not panic")
		}
	}()
	Register(reservedDomain{})
}

type reservedDomain struct{ demoDomain }

func (reservedDomain) Info() DomainInfo {
	return DomainInfo{Scheme: "search", Identity: Identity{Binary: "search"}}
}

// --- host ---

func TestHostGetMintLinks(t *testing.T) {
	registerDemo(t)
	h, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := ParseURI("demo://widget/42")
	rec, err := h.Get(context.Background(), u)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	w, ok := rec.(widget)
	if !ok {
		t.Fatalf("Get returned %T, want widget", rec)
	}
	if w.ID != "42" || w.Name != "Widget 42" {
		t.Errorf("Get = %+v", w)
	}

	minted, err := h.Mint(w)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if minted.String() != "demo://widget/42" {
		t.Errorf("Mint = %q, want demo://widget/42", minted.String())
	}

	links := h.Links(w)
	if len(links) != 1 || links[0].String() != "demo://maker/m1" {
		t.Errorf("Links = %v, want [demo://maker/m1]", links)
	}
}

func TestHostListAndMintGating(t *testing.T) {
	registerDemo(t)
	h, _ := Open()
	u, _ := ParseURI("demo://widget/42")

	parts, err := h.List(context.Background(), u, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("List returned %d parts, want 1", len(parts))
	}
	p, ok := parts[0].(part)
	if !ok || p.WidgetID != "42" {
		t.Errorf("List part = %+v", parts[0])
	}

	// The widget resolver mints; the part type, only ever a list child, does not.
	if _, err := h.Mint(widget{ID: "42"}); err != nil {
		t.Errorf("Mint(widget) = %v, want nil", err)
	}
	if _, err := h.Mint(part{ID: "p1"}); err == nil {
		t.Error("Mint(part) = nil error, want unmintable")
	}
}

func TestHostResolveAndLocate(t *testing.T) {
	registerDemo(t)
	h, _ := Open()

	got, err := h.Resolve("demo://widget/42")
	if err != nil || got.String() != "demo://widget/42" {
		t.Errorf("Resolve(uri) = %q, %v", got.String(), err)
	}

	got, err = h.Resolve("https://demo.example/maker/7")
	if err != nil || got.String() != "demo://maker/7" {
		t.Errorf("Resolve(url) = %q, %v", got.String(), err)
	}

	got, err = h.ResolveOn("demo", "99")
	if err != nil || got.String() != "demo://widget/99" {
		t.Errorf("ResolveOn(bare id) = %q, %v", got.String(), err)
	}

	loc, err := h.Locate(got)
	if err != nil || loc != "https://demo.example/widget/99" {
		t.Errorf("Locate = %q, %v", loc, err)
	}

	if _, err := h.Resolve("just-a-bare-id"); err == nil {
		t.Error("Resolve(bare id) = nil error, want ambiguity error")
	}
}

func TestHostWrapEnvelope(t *testing.T) {
	registerDemo(t)
	h, _ := Open()
	at := time.Unix(1700000000, 0)
	env, err := h.Wrap(widget{ID: "42", Name: "Widget 42", MakerID: "m1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "demo://widget/42" || env.Type != "demo/widget" {
		t.Errorf("envelope id/type = %q/%q", env.ID, env.Type)
	}
	if env.Fetched == "" {
		t.Error("envelope @fetched empty")
	}
	if got := env.Links["maker_id"]; len(got) != 1 || got[0] != "demo://maker/m1" {
		t.Errorf("envelope links = %v", env.Links)
	}
}

func TestHostSearch(t *testing.T) {
	registerDemo(t)
	h, _ := Open()

	if !h.Searchable("demo") {
		t.Fatal("Searchable(demo) = false, want true")
	}
	if h.Searchable("nope") {
		t.Error("Searchable(nope) = true, want false")
	}

	recs, err := h.Search(context.Background(), "demo", "shiny gears", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("Search returned %d records, want 1", len(recs))
	}
	w, ok := recs[0].(widget)
	if !ok {
		t.Fatalf("Search record is %T, want widget", recs[0])
	}
	if w.Name != "Hit for shiny gears" {
		t.Errorf("Search hit name = %q", w.Name)
	}

	if _, err := h.Search(context.Background(), "nope", "x", 0); err == nil {
		t.Error("Search(unknown domain) = nil error, want usage error")
	}
}
