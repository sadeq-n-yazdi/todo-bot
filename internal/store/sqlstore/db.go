// Package sqlstore implements store.Store in SQL shared by all providers. A
// provider supplies a Dialect for the few places where databases differ.
package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// DB implements store.Store. A DB handed to the callback of WithTx is bound
// to the transaction instead of the pool.
type DB struct {
	d    Dialect
	db   *sql.DB // nil for a transaction-bound DB
	q    Querier // rebinds placeholders for the dialect
	inTx bool
}

// rebinder rewrites placeholders on every statement.
type rebinder struct {
	q Querier
	d Dialect
}

func (r rebinder) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return r.q.ExecContext(ctx, r.d.Rebind(query), args...)
}

func (r rebinder) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.q.QueryContext(ctx, r.d.Rebind(query), args...)
}

func (r rebinder) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.q.QueryRowContext(ctx, r.d.Rebind(query), args...)
}

// New wraps an open pool. It does not migrate; call Migrate.
func New(d Dialect, db *sql.DB) *DB {
	return &DB{d: d, db: db, q: rebinder{db, d}}
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
		return fmt.Errorf("sqlstore: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(&DB{d: d.d, q: rebinder{tx, d.d}, inTx: true}); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: commit: %w", err)
	}
	return nil
}

var (
	_ store.Store    = (*DB)(nil)
	_ store.Migrator = (*DB)(nil)
)

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
	return fmt.Errorf("sqlstore: %s %v: %w", what, key, store.ErrNotFound)
}

// checkAffected turns an Exec result into nil, store.ErrNotFound (no row
// touched) or a wrapped error.
func checkAffected(res sql.Result, err error, op, what string, key any) error {
	if err != nil {
		return fmt.Errorf("sqlstore: %s %v: %w", op, key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlstore: %s %v: %w", op, key, err)
	}
	if n == 0 {
		return notFound(what, key)
	}
	return nil
}

// minTime scans a single nullable MIN(...) value.
func (d *DB) minTime(ctx context.Context, query string, args ...any) (time.Time, bool, error) {
	var n sql.NullInt64
	if err := d.q.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return time.Time{}, false, fmt.Errorf("sqlstore: next time: %w", err)
	}
	if !n.Valid {
		return time.Time{}, false, nil
	}
	return fromUnix(n.Int64), true, nil
}

// affected returns the number of rows an Exec touched.
func affected(res sql.Result, err error, op string) (int64, error) {
	if err != nil {
		return 0, fmt.Errorf("sqlstore: %s: %w", op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlstore: %s: %w", op, err)
	}
	return n, nil
}
