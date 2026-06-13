package kit

// Importing kit registers the SQLite store backend, so --db out.db works in
// every kit CLI with no extra wiring. SQLite is the default scheme for a
// bare-path DSN. Additional backends (DuckDB, Postgres) register themselves the
// same way: a CLI blank-imports the backend package to enable its scheme, and
// the DSN selects it (duckdb:..., postgres://...).
import (
	_ "github.com/tamnd/any-cli/kit/store/sqlitestore"
)
