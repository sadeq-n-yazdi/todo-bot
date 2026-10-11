package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite"
)

// The backup open must not change the journal mode of the source database.
func TestOpenExistingKeepsJournalMode(t *testing.T) {
	for _, mode := range []string{"delete", "wal"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "x.db")
			raw, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := raw.Exec("PRAGMA journal_mode=" + mode); err != nil {
				t.Fatal(err)
			}
			if _, err := raw.Exec("CREATE TABLE t (a int)"); err != nil {
				t.Fatal(err)
			}
			_ = raw.Close()
			if got := rawPragma(t, path, "journal_mode"); got != mode {
				t.Fatalf("setup: journal_mode = %q, want %q", got, mode)
			}

			d, err := sqlite.OpenExisting(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Backup(ctx, filepath.Join(t.TempDir(), "b.db")); err != nil {
				t.Fatal(err)
			}
			_ = d.Close()
			if got := rawPragma(t, path, "journal_mode"); got != mode {
				t.Errorf("journal_mode after backup = %q, want %q", got, mode)
			}
		})
	}
}
