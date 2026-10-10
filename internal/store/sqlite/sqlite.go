// Package sqlite implements store.Store on modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// DB implements store.Store. Its pool has exactly one connection.
type DB struct {
	db *sql.DB //nolint:unused // used from #4
}

var errNotImplemented = errors.New("sqlite: not implemented")

// Open opens (creating if needed) the database at path with WAL, foreign keys
// and busy_timeout pragmas, and applies pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	return nil, errNotImplemented // TODO(#4)
}

// Migrate applies pending migrations from the migrations package. Open calls
// it; it is exported for the "migrate" subcommand and tests.
func (d *DB) Migrate(ctx context.Context) error { return errNotImplemented }

var _ store.Store = (*DB)(nil)
