// Package sqlite is the SQLite provider: a sqlstore.Dialect plus Open, Backup
// and the "sqlite" URL scheme registration. It uses modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlstore"
)

func init() { store.Register("sqlite", opener) }

// DB is a sqlstore.DB on SQLite, plus Backup.
type DB struct {
	*sqlstore.DB
	raw *sql.DB
}

var (
	_ store.Store    = (*DB)(nil)
	_ store.Backuper = (*DB)(nil)
	_ store.Migrator = (*DB)(nil)
)

// _txlock=immediate makes every transaction BEGIN IMMEDIATE, so handles on
// one file wait for each other (busy_timeout) instead of failing with
// SQLITE_BUSY when a read transaction tries to write.
const dsnParams = "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)"

var pathEscaper = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")

func dsn(path string) string { return "file:" + pathEscaper.Replace(path) + dsnParams }

// Open opens (creating if needed) the database at path with WAL, foreign keys
// and busy_timeout pragmas, and applies pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	sdb, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	sdb.SetMaxOpenConns(1)
	if err := sdb.PingContext(ctx); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("sqlite: ping %s: %w", path, err)
	}
	d := &DB{DB: sqlstore.New(dialect{}, sdb), raw: sdb}
	if err := d.Migrate(ctx); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	return d, nil
}

// opener implements store.Opener. An absolute path is u.Path
// (sqlite:///var/lib/x.db), a relative one u.Opaque (sqlite:todo.db). A host
// is rejected. Errors never contain the URL.
func opener(ctx context.Context, u *url.URL) (store.Store, error) {
	if u.Host != "" {
		return nil, errors.New("sqlite: database URL must not have a host (use sqlite:///abs/path or sqlite:rel/path)")
	}
	path := u.Path
	if u.Opaque != "" {
		path = u.Opaque
		if p, err := url.PathUnescape(path); err == nil {
			path = p
		}
	}
	if path == "" {
		return nil, errors.New("sqlite: database URL has no path")
	}
	return Open(ctx, path)
}

// Backup writes a consistent copy of the database to path using VACUUM INTO.
// The target file must not exist.
func (d *DB) Backup(ctx context.Context, path string) error {
	if _, err := d.raw.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("sqlite: backup to %s: %w", path, err)
	}
	return nil
}
