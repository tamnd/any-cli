package kit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/tamnd/any-cli/kit/store"
	_ "modernc.org/sqlite"
)

// repo is a tiny domain record with a primary key for the store tee.
type repo struct {
	ID    string `json:"id" kit:"id" table:"id"`
	Owner string `json:"owner" table:"owner"`
	Stars int    `json:"stars" table:"stars"`
}

// searchIn declares one positional arg and one flag.
type searchIn struct {
	Query string `kit:"arg" help:"search text"`
	Limit int    `kit:"flag" help:"max repos" default:"50"`
}

func newTestApp() *App {
	app := New(Identity{Binary: "demo", Short: "demo cli", Version: "0.0.1"})
	Handle(app, OpMeta{
		Name:    "search",
		Group:   "read",
		Summary: "search repos",
		Args:    []Arg{{Name: "query", Help: "search text"}},
	}, func(_ context.Context, in searchIn, emit func(repo) error) error {
		n := in.Limit
		if n == 0 || n > 3 {
			n = 3
		}
		for i := 0; i < n; i++ {
			if err := emit(repo{ID: in.Query + "-" + string(rune('a'+i)), Owner: in.Query, Stars: i}); err != nil {
				return err
			}
		}
		return nil
	})
	return app
}

func TestInvokeCollectsRecords(t *testing.T) {
	app := newTestApp()
	op := app.byName["search"]
	if op == nil {
		t.Fatal("search op not registered")
	}
	sink := &collectSink{}
	in := Input{Args: []string{"go"}, Flags: map[string]any{}}
	if err := op.Invoke(context.Background(), in, RunContext{}, sink); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(sink.recs) != 3 {
		t.Fatalf("want 3 records, got %d", len(sink.recs))
	}
	first, ok := sink.recs[0].(repo)
	if !ok {
		t.Fatalf("record type %T", sink.recs[0])
	}
	if first.ID != "go-a" || first.Owner != "go" {
		t.Fatalf("unexpected first record: %+v", first)
	}
}

func TestLimitStopsStream(t *testing.T) {
	app := newTestApp()
	op := app.byName["search"]
	sink := &collectSink{}
	in := Input{Args: []string{"rust"}, Flags: map[string]any{}}
	if err := op.Invoke(context.Background(), in, RunContext{Limit: 2}, sink); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(sink.recs) != 2 {
		t.Fatalf("limit 2 should yield 2 records, got %d", len(sink.recs))
	}
}

func TestFlagDefaultApplies(t *testing.T) {
	app := newTestApp()
	op := app.byName["search"]
	sink := &collectSink{}
	// No limit flag set: the default "50" caps to the handler's own max of 3.
	in := Input{Args: []string{"c"}, Flags: map[string]any{}}
	if err := op.Invoke(context.Background(), in, RunContext{}, sink); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(sink.recs) != 3 {
		t.Fatalf("want 3, got %d", len(sink.recs))
	}
}

func TestStoreTee(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "out.db")
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	app := newTestApp()
	op := app.byName["search"]
	sink := &collectSink{}
	in := Input{Args: []string{"go"}, Flags: map[string]any{}}
	if err := op.Invoke(context.Background(), in, RunContext{Store: st}, sink); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen the file directly and count the teed rows.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM repo`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("want 3 teed rows, got %d", n)
	}
	var owner string
	if err := db.QueryRow(`SELECT json_extract(data,'$.owner') FROM repo WHERE id='go-a'`).Scan(&owner); err != nil {
		t.Fatalf("select: %v", err)
	}
	if owner != "go" {
		t.Fatalf("teed owner = %q, want go", owner)
	}
}

func TestNoResultsError(t *testing.T) {
	app := New(Identity{Binary: "demo"})
	Handle(app, OpMeta{Name: "empty", Summary: "emits nothing"},
		func(_ context.Context, _ struct{}, _ func(repo) error) error { return nil })
	op := app.byName["empty"]
	err := op.Invoke(context.Background(), Input{Flags: map[string]any{}}, RunContext{}, &collectSink{})
	if err == nil {
		t.Fatal("want a no-results error")
	}
}

func TestInputSchema(t *testing.T) {
	app := newTestApp()
	op := app.byName["search"]
	schema := op.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("no properties: %v", schema)
	}
	if _, ok := props["query"]; !ok {
		t.Fatal("missing query property")
	}
	if _, ok := props["limit"]; !ok {
		t.Fatal("missing limit property")
	}
	req, _ := schema["required"].([]string)
	if len(req) != 1 || req[0] != "query" {
		t.Fatalf("required = %v, want [query]", req)
	}
}
