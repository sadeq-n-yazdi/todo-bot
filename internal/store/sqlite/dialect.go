package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlstore"
)

// dialect is the SQLite sqlstore.Dialect.
type dialect struct{}

func (dialect) Name() string { return "sqlite" }

func (dialect) Rebind(query string) string { return query }

func (dialect) InsertID(ctx context.Context, q sqlstore.Querier, query string, args ...any) (int64, error) {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (dialect) AllocTodoNum(ctx context.Context, q sqlstore.Querier, chatID int64) (int, error) {
	var num int
	err := q.QueryRowContext(ctx,
		"UPDATE chats SET next_num = next_num + 1 WHERE id = ? RETURNING next_num - 1", chatID).Scan(&num)
	return num, err
}

func insertInto(table string, cols []string) string {
	return "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
}

func (dialect) Upsert(table string, cols, keys, update []string) string {
	sets := make([]string, len(update))
	for i, c := range update {
		sets[i] = c + " = excluded." + c
	}
	return insertInto(table, cols) + " ON CONFLICT (" + strings.Join(keys, ", ") + ") DO UPDATE SET " +
		strings.Join(sets, ", ")
}

func (dialect) InsertIgnore(table string, cols, keys []string) string {
	return insertInto(table, cols) + " ON CONFLICT (" + strings.Join(keys, ", ") + ") DO NOTHING"
}

func (dialect) ClaimSubquery(sub string) string { return sub }

// LockMigrations is a no-op: every migration runs in an IMMEDIATE transaction
// (DSN _txlock=immediate), which serialises runners on the file.
func (dialect) LockMigrations(context.Context, *sql.DB) (func(), error) { return func() {}, nil }

func (dialect) CreateMigrationsTable() string {
	return `CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL,
  applied_at INTEGER NOT NULL
)`
}

func (dialect) LegacyVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}
