// Package sqlitestore is the built-in default store backend. It is pure Go (no
// cgo) via modernc.org/sqlite, so it adds no build requirement to a kit binary.
// Importing it registers the "sqlite" scheme; kit imports it by default, so any
// kit CLI can persist with --db x.db out of the box. Future file/SQL backends
// (DuckDB, Postgres) live in sibling packages a consumer opts into.
package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tamnd/any-cli/kit/store"
	_ "modernc.org/sqlite"
)

func init() { store.Register("sqlite", Open) }

// Open opens (creating if needed) a SQLite database at path. Each record
// collection becomes a table of (id PRIMARY KEY, data JSON, updated_at); this
// generic shape stores any record type and is identical across SQL backends, so
// switching to DuckDB or Postgres changes only the driver, not the schema. A
// domain that wants a relational schema supplies its own store.Store instead.
func Open(ctx context.Context, path string) (store.Store, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite: empty database path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("sqlite: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // a single command run writes sequentially
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	return &sqliteStore{db: db, tables: map[string]bool{}}, nil
}

type sqliteStore struct {
	db     *sql.DB
	mu     sync.Mutex
	tables map[string]bool
}

func (s *sqliteStore) ensure(ctx context.Context, table string) error {
	if s.tables[table] {
		return nil
	}
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`, table)
	if _, err := s.db.ExecContext(ctx, q); err != nil {
		return err
	}
	s.tables[table] = true
	return nil
}

func (s *sqliteStore) Upsert(ctx context.Context, collection, id string, data []byte) error {
	table := store.Collection(collection)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(ctx, table); err != nil {
		return fmt.Errorf("sqlite: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if id == "" {
		// No primary key: append with a rowid-backed synthetic key.
		q := fmt.Sprintf(`INSERT INTO %q (id, data, updated_at) VALUES (lower(hex(randomblob(16))), ?, ?)`, table)
		_, err := s.db.ExecContext(ctx, q, string(data), now)
		return wrap(err)
	}
	q := fmt.Sprintf(`INSERT INTO %q (id, data, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`, table)
	_, err := s.db.ExecContext(ctx, q, id, string(data), now)
	return wrap(err)
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("sqlite: %w", err)
}
