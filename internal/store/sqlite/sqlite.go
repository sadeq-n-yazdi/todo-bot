// Package sqlite is the SQLite provider: a sqlstore.Dialect plus Open, Backup
// and the "sqlite" URL scheme registration. It uses modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	modernc "modernc.org/sqlite" // registers the "sqlite" driver

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

// dsnExisting is dsn with mode=rw: SQLite then fails on a missing file
// instead of creating it.
func dsnExisting(path string) string {
	return "file:" + pathEscaper.Replace(path) + dsnParams + "&mode=rw"
}

// OpenExisting opens the database at path, which must exist, with the same
// pragmas as Open. It does not create the file and does not apply migrations.
func OpenExisting(ctx context.Context, path string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("sqlite: database %s: %w", path, err)
	}
	sdb, err := sql.Open("sqlite", dsnExisting(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	sdb.SetMaxOpenConns(1)
	if err := ping(ctx, sdb); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("sqlite: ping %s: %w", path, err)
	}
	return &DB{DB: sqlstore.New(dialect{}, sdb), raw: sdb}, nil
}

// Open opens (creating if needed) the database at path with WAL, foreign keys
// and busy_timeout pragmas, and applies pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	sdb, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	sdb.SetMaxOpenConns(1)
	if err := ping(ctx, sdb); err != nil {
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

const (
	pingTries = 50
	pingWait  = 20 * time.Millisecond
)

// isBusy reports SQLITE_BUSY (5) or an extended code of it, by driver code.
func isBusy(err error) bool {
	var se *modernc.Error
	return errors.As(err, &se) && se.Code()&0xff == 5
}

// ping connects, retrying on SQLITE_BUSY for about a second. Several
// processes opening a brand-new file at once can hit BUSY in the connection
// pragmas (journal_mode=WAL), which busy_timeout does not cover.
func ping(ctx context.Context, db *sql.DB) error {
	var err error
	for range pingTries {
		if err = db.PingContext(ctx); err == nil || !isBusy(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pingWait):
		}
	}
	return err
}

// opener implements store.Opener.
func opener(ctx context.Context, u *url.URL) (store.Store, error) {
	path, err := urlPath(u)
	if err != nil {
		return nil, err
	}
	d, err := Open(ctx, path)
	if err != nil {
		return nil, err // not "return Open(...)": that would wrap a nil *DB in a non-nil interface
	}
	return d, nil
}

// OpenExistingURL is OpenExisting for a database URL of the sqlite scheme,
// with the same URL rules as store.Open. It is not reachable through
// store.Open, so only code that calls it, such as the backup command, can
// open a database without migrating it.
func OpenExistingURL(ctx context.Context, raw string) (*DB, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("sqlite: invalid database URL")
	}
	if u.Scheme != "sqlite" {
		return nil, errors.New("sqlite: database URL must use the sqlite scheme")
	}
	path, err := urlPath(u)
	if err != nil {
		return nil, err
	}
	return OpenExisting(ctx, path)
}

// urlPath returns the file path of a sqlite URL. An absolute path is u.Path
// (sqlite:///var/lib/x.db), a relative one u.Opaque (sqlite:todo.db). A host
// is rejected, and so are credentials, a query and a fragment. Errors never
// contain the URL.
func urlPath(u *url.URL) (string, error) {
	if u.User != nil {
		return "", errors.New("sqlite: database URL must not have credentials")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("sqlite: database URL must not have a query or fragment")
	}
	if u.Host != "" {
		return "", errors.New("sqlite: database URL must not have a host (use sqlite:///abs/path or sqlite:rel/path)")
	}
	path := u.Path
	if u.Opaque != "" {
		path = u.Opaque
		if p, err := url.PathUnescape(path); err == nil {
			path = p
		}
	}
	if path == "" {
		return "", errors.New("sqlite: database URL has no path")
	}
	return path, nil
}

// Backup writes a consistent copy of the database to path using VACUUM INTO.
// The target file must not exist.
func (d *DB) Backup(ctx context.Context, path string) error {
	if _, err := d.raw.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("sqlite: backup to %s: %w", path, err)
	}
	return nil
}
