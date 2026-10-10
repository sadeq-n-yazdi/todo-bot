package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/migrations"
)

type migration struct {
	version int
	name    string
}

// Migrate applies pending migrations from the migrations package. Open calls
// it; it is exported for the "migrate" subcommand and tests. Each migration
// runs in its own transaction together with the user_version update.
func (d *DB) Migrate(ctx context.Context) error {
	if d.inTx {
		return errors.New("sqlite: migrate inside a transaction")
	}
	list, err := listMigrations(migrations.FS)
	if err != nil {
		return err
	}
	var current int
	if err := d.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("sqlite: read user_version: %w", err)
	}
	for _, m := range list {
		if m.version <= current {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, m.name)
		if err != nil {
			return fmt.Errorf("sqlite: read %s: %w", m.name, err)
		}
		if err := d.applyMigration(ctx, m, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) applyMigration(ctx context.Context, m migration, body string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin %s: %w", m.name, err)
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("sqlite: apply %s: %w", m.name, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("sqlite: set user_version for %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit %s: %w", m.name, err)
	}
	return nil
}

func listMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list migrations: %w", err)
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
			return nil, fmt.Errorf("sqlite: bad migration name %q", name)
		}
		out = append(out, migration{version: v, name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
