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

type idArg struct {
	ID string `kit:"arg"`
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
