package store

import "context"

// Backuper is implemented by providers that can write a consistent copy of
// the database to a local file (SQLite: VACUUM INTO). Others use their own
// tools (pg_dump, mysqldump).
type Backuper interface {
	Backup(ctx context.Context, path string) error
}

// Migrator is implemented by providers whose schema is migrated by the bot.
type Migrator interface {
	Migrate(ctx context.Context) error
}
