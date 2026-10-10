package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/migrations"
)

type migration struct {
	version int
	name    string
}

// Migrate applies pending migrations from the migrations package and records
// them in schema_migrations. Open calls it; it is exported for the "migrate"
// subcommand and tests. It holds the dialect's migration lock, and each
// migration runs in its own transaction together with its schema_migrations
// row, so two nodes starting together cannot apply one twice.
//
// A database that predates schema_migrations is seeded from the dialect's
// legacy version (SQLite: PRAGMA user_version).
func (d *DB) Migrate(ctx context.Context) error {
	if d.inTx {
		return errors.New("sqlstore: migrate inside a transaction")
	}
	mfs, err := migrations.For(d.d.Name())
	if err != nil {
		return err
	}
	list, err := listMigrations(mfs)
	if err != nil {
		return err
	}
	release, err := d.d.LockMigrations(ctx, d.db)
	if err != nil {
		return fmt.Errorf("sqlstore: lock migrations: %w", err)
	}
	defer release()

	if _, err := d.db.ExecContext(ctx, d.d.CreateMigrationsTable()); err != nil {
		return fmt.Errorf("sqlstore: create schema_migrations: %w", err)
	}
	if err := d.seedMigrations(ctx, list); err != nil {
		return err
	}
	for _, m := range list {
		body, err := fs.ReadFile(mfs, m.name)
		if err != nil {
			return fmt.Errorf("sqlstore: read %s: %w", m.name, err)
		}
		if err := d.applyMigration(ctx, m, string(body)); err != nil {
			return err
		}
	}
	return nil
}

// seedMigrations records the migrations an old database already has, taken
// from the dialect's legacy version. It does nothing once schema_migrations
// has any row.
func (d *DB) seedMigrations(ctx context.Context, list []migration) error {
	legacy, err := d.d.LegacyVersion(ctx, d.db)
	if err != nil {
		return fmt.Errorf("sqlstore: read legacy schema version: %w", err)
	}
	if legacy == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := rebinder{tx, d.d}
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		return fmt.Errorf("sqlstore: seed migrations: %w", err)
	}
	if n > 0 {
		return nil
	}
	now := time.Now().Unix()
	for _, m := range list {
		if m.version > legacy {
			continue
		}
		if _, err := q.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)", m.version, m.name, now); err != nil {
			return fmt.Errorf("sqlstore: seed %s: %w", m.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: commit seed: %w", err)
	}
	return nil
}

func (d *DB) applyMigration(ctx context.Context, m migration, body string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: begin %s: %w", m.name, err)
	}
	defer func() { _ = tx.Rollback() }()
	q := rebinder{tx, d.d}
	// Checked inside the transaction: another node may have applied it
	// while this one waited for the lock.
	var done int
	err = q.QueryRowContext(ctx, "SELECT 1 FROM schema_migrations WHERE version = ?", m.version).Scan(&done)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("sqlstore: check %s: %w", m.name, err)
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("sqlstore: apply %s: %w", m.name, err)
	}
	if _, err := q.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
		m.version, m.name, time.Now().Unix()); err != nil {
		return fmt.Errorf("sqlstore: record %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: commit %s: %w", m.name, err)
	}
	return nil
}

func listMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("sqlstore: list migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("sqlstore: bad migration name %q", name)
		}
		out = append(out, migration{version: v, name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
