package sqlstore

import (
	"context"
	"database/sql"
)

// Querier is satisfied by both *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Dialect holds the few places where SQL differs between databases. All SQL
// handed to or returned by a Dialect uses "?" placeholders; sqlstore applies
// Rebind before running a statement.
type Dialect interface {
	// Name is the migrations directory name ("sqlite").
	Name() string
	// Rebind converts "?" placeholders to the database's own form.
	Rebind(query string) string
	// InsertID runs an INSERT and returns the generated integer id.
	InsertID(ctx context.Context, q Querier, query string, args ...any) (int64, error)
	// AllocTodoNum atomically returns the chat's next todo number and
	// advances it. It returns sql.ErrNoRows if the chat does not exist.
	AllocTodoNum(ctx context.Context, q Querier, chatID int64) (int, error)
	// Upsert builds an insert that, on a key conflict, overwrites the update columns.
	Upsert(table string, cols, keys, update []string) string
	// InsertIgnore builds an insert that does nothing on a key conflict.
	InsertIgnore(table string, cols, keys []string) string
	// ClaimSubquery adapts the "SELECT id ... ORDER BY ... LIMIT ?" subquery
	// used by the claim UPDATE (MySQL needs it wrapped in a derived table).
	ClaimSubquery(sub string) string
	// Greatest returns an expression for the larger of two scalar SQL
	// expressions (SQLite max(a, b); Postgres and MySQL GREATEST(a, b)).
	Greatest(a, b string) string
	// LockMigrations serialises migration runners on one database and
	// returns the release function. SQLite needs no lock: every migration
	// runs in an IMMEDIATE transaction.
	LockMigrations(ctx context.Context, db *sql.DB) (release func(), err error)
	// CreateMigrationsTable is the DDL creating schema_migrations if missing.
	CreateMigrationsTable() string
	// LegacyVersion returns the schema version tracked outside
	// schema_migrations (SQLite: PRAGMA user_version), 0 if none.
	LegacyVersion(ctx context.Context, db *sql.DB) (int, error)
	// RecordVersion is called inside each migration's transaction after the
	// schema_migrations row is written. SQLite keeps PRAGMA user_version in
	// step so an older binary does not re-run migrations; others do nothing.
	RecordVersion(ctx context.Context, q Querier, version int) error
}
