// Package store defines the pluggable record-store SPI that lets any kit read
// double as a crawl: with --db set, every emitted record is upserted into a
// local store before it reaches the output.
//
// The SPI is backend-neutral. A record reaches a Store already marshalled to
// JSON with its primary key extracted, so a backend never reflects over domain
// types and the same interface serves SQLite (the built-in default), DuckDB,
// Postgres, or anything else. Backends register themselves under a URL scheme
// with Register; callers open one with Open and a DSN.
//
// DSN forms:
//
//	x.db                      bare path  -> the default scheme (sqlite)
//	sqlite:/var/data/x.db     explicit scheme + path
//	duckdb:x.duckdb           another file backend
//	postgres://user@host/db   a full URL is passed through verbatim
package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store persists records. Implementations must be safe for sequential use by a
// single command run; concurrent use is the caller's responsibility.
type Store interface {
	// Upsert writes one record into the named collection, keyed by id. data is
	// the record marshalled to JSON. An empty id means "append" (the backend
	// supplies a key).
	Upsert(ctx context.Context, collection, id string, data []byte) error
	// Close flushes and releases the backend (closing the file/connection).
	Close() error
}

// Opener opens a Store from the backend-specific target parsed out of a DSN
// (the part after the scheme for file backends, or the full DSN for URL
// backends).
type Opener func(ctx context.Context, target string) (Store, error)

var (
	mu            sync.RWMutex
	openers       = map[string]Opener{}
	defaultScheme = "sqlite"
)

// Register installs a backend under a scheme. Backends call this from an init
// function so a blank import wires them in. Registering a scheme twice panics,
// which surfaces a duplicate-driver bug at startup rather than silently.
func Register(scheme string, open Opener) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := openers[scheme]; dup {
		panic("kit/store: scheme registered twice: " + scheme)
	}
	openers[scheme] = open
}

// SetDefaultScheme changes the scheme used for a bare-path DSN (default
// "sqlite"). A consumer that prefers another built-in backend can call this.
func SetDefaultScheme(scheme string) {
	mu.Lock()
	defer mu.Unlock()
	defaultScheme = scheme
}

// Schemes lists the registered backend schemes, sorted.
func Schemes() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(openers))
	for s := range openers {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ParseDSN splits a DSN into its scheme and backend target. A URL-style DSN
// ("scheme://...") keeps the whole string as the target so the URL backend gets
// everything it needs. A "scheme:target" DSN whose scheme is registered splits
// on the first colon. Anything else is a bare path under the default scheme.
func ParseDSN(dsn string) (scheme, target string) {
	if i := strings.Index(dsn, "://"); i >= 0 {
		return dsn[:i], dsn
	}
	if i := strings.IndexByte(dsn, ':'); i > 1 { // i>1 so a Windows drive letter is not a scheme
		s := dsn[:i]
		mu.RLock()
		_, known := openers[s]
		mu.RUnlock()
		if known {
			return s, dsn[i+1:]
		}
	}
	mu.RLock()
	def := defaultScheme
	mu.RUnlock()
	return def, dsn
}

// Open resolves the DSN to a backend and opens a Store. It returns a clear
// error naming the available schemes when the backend is not registered (the
// common case being a DuckDB or Postgres DSN in a build that did not import the
// backend).
func Open(ctx context.Context, dsn string) (Store, error) {
	scheme, target := ParseDSN(dsn)
	mu.RLock()
	open := openers[scheme]
	mu.RUnlock()
	if open == nil {
		return nil, fmt.Errorf("no store backend for scheme %q (have: %s); import the backend package to enable it",
			scheme, strings.Join(Schemes(), ", "))
	}
	return open(ctx, target)
}

// Collection turns a record type name into a safe collection (table) name:
// snake_case, alphanumerics and underscores only. It is exported so backends
// and the core agree on the same normalization.
func Collection(name string) string {
	var b strings.Builder
	prevUnder := false
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && !prevUnder {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			prevUnder = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnder = false
		default:
			if !prevUnder && b.Len() > 0 {
				b.WriteByte('_')
				prevUnder = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "records"
	}
	return out
}
