// Package sqlite implements store.Store on modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB implements store.Store. Its pool has exactly one connection. A DB handed
// to the callback of WithTx is bound to the transaction instead of the pool.
type DB struct {
	db   *sql.DB // nil for a transaction-bound DB
	q    querier
	inTx bool
}

const dsnPragmas = "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)"

var pathEscaper = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")

// Open opens (creating if needed) the database at path with WAL, foreign keys
// and busy_timeout pragmas, and applies pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	sdb, err := sql.Open("sqlite", "file:"+pathEscaper.Replace(path)+dsnPragmas)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	sdb.SetMaxOpenConns(1)
	d := &DB{db: sdb, q: sdb}
	if err := sdb.PingContext(ctx); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("sqlite: ping %s: %w", path, err)
	}
	if err := d.Migrate(ctx); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the database. On a transaction-bound DB it does nothing.
func (d *DB) Close() error {
	if d.inTx {
		return nil
	}
	return d.db.Close()
}

// WithTx runs fn in a transaction, rolling back on error or panic. A nested
// call on a transaction-bound store just runs fn with it.
func (d *DB) WithTx(ctx context.Context, fn func(tx store.Store) error) error {
	if d.inTx {
		return fn(d)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(&DB{q: tx, inTx: true}); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	return nil
}

// Backup writes a consistent copy of the database to path using VACUUM INTO.
// The target file must not exist.
func (d *DB) Backup(ctx context.Context, path string) error {
	if _, err := d.q.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("sqlite: backup to %s: %w", path, err)
	}
	return nil
}

var _ store.Store = (*DB)(nil)

// ---- helpers ----

type scanner interface{ Scan(dest ...any) error }

func fromUnix(x int64) time.Time { return time.Unix(x, 0).UTC() }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func ptrTime(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := fromUnix(n.Int64)
	return &t
}

// unixOrNow returns t as Unix seconds, or the current time when t is zero.
func unixOrNow(t time.Time) int64 {
	if t.IsZero() {
		return time.Now().Unix()
	}
	return t.Unix()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func notFound(what string, key any) error {
	return fmt.Errorf("sqlite: %s %v: %w", what, key, store.ErrNotFound)
}

// checkAffected turns an Exec result into nil, store.ErrNotFound (no row
// touched) or a wrapped error.
func checkAffected(res sql.Result, err error, op, what string, key any) error {
	if err != nil {
		return fmt.Errorf("sqlite: %s %v: %w", op, key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: %s %v: %w", op, key, err)
	}
	if n == 0 {
		return notFound(what, key)
	}
	return nil
}

func (d *DB) minTime(ctx context.Context, query string) (time.Time, bool, error) {
	var n sql.NullInt64
	if err := d.q.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return time.Time{}, false, fmt.Errorf("sqlite: next time: %w", err)
	}
	if !n.Valid {
		return time.Time{}, false, nil
	}
	return fromUnix(n.Int64), true, nil
}
