// Package store defines the persistence contract. The only implementation is
// internal/store/sqlite. All times are passed and returned in UTC.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// ErrNotFound is returned (possibly wrapped) when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store is the persistence API.
type Store interface {
	GetUser(ctx context.Context, id int64) (model.User, error)
	UpsertUser(ctx context.Context, u model.User) error

	// EnsureChat inserts the chat if missing, or updates its title.
	EnsureChat(ctx context.Context, c model.Chat) error
	GetChat(ctx context.Context, id int64) (model.Chat, error)
	// SetDigest sets digest_time/digest_by/digest_next_at; hhmm "" and next nil turn it off.
	SetDigest(ctx context.Context, chatID int64, hhmm string, by int64, next *time.Time) error
	// ListChatsByDigestUser returns chats whose digest was set by userID.
	ListChatsByDigestUser(ctx context.Context, userID int64) ([]model.Chat, error)
	// MigrateChat moves a chat and all its todos and reminders to newID in one transaction.
	MigrateChat(ctx context.Context, oldID, newID int64) error

	// InsertTodo assigns t.ID and the next per-chat t.Num atomically.
	InsertTodo(ctx context.Context, t *model.Todo) error
	GetTodo(ctx context.Context, id int64) (model.Todo, error)
	GetTodoByNum(ctx context.Context, chatID int64, num int) (model.Todo, error)
	// ListTodos orders by (due_at IS NULL, due_at, num).
	ListTodos(ctx context.Context, q model.TodoQuery) ([]model.Todo, error)
	CountTodos(ctx context.Context, q model.TodoQuery) (int, error)
	UpdateTodo(ctx context.Context, t model.Todo) error
	// ListRecurringByCreator returns open recurring todos created by userID.
	ListRecurringByCreator(ctx context.Context, userID int64) ([]model.Todo, error)

	// InsertReminder assigns r.ID.
	InsertReminder(ctx context.Context, r *model.Reminder) error
	GetReminder(ctx context.Context, id int64) (model.Reminder, error)
	UpdateReminder(ctx context.Context, r model.Reminder) error
	// CancelPending cancels pending reminders of a todo; no kinds = all kinds.
	CancelPending(ctx context.Context, todoID int64, kinds ...model.ReminderKind) error
	// DueReminders returns pending reminders with fire_at <= now, oldest first.
	DueReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error)
	// NextReminderAt returns the earliest pending fire_at, ok=false if none.
	NextReminderAt(ctx context.Context) (t time.Time, ok bool, err error)

	// DueDigests returns chats with digest_next_at <= now.
	DueDigests(ctx context.Context, now time.Time) ([]model.Chat, error)
	NextDigestAt(ctx context.Context) (t time.Time, ok bool, err error)

	// WithTx runs fn in a transaction. Inside fn use ONLY tx: the pool has a
	// single connection, so using the outer Store deadlocks.
	WithTx(ctx context.Context, fn func(tx Store) error) error
	// Backup writes a consistent copy of the database to path (VACUUM INTO).
	Backup(ctx context.Context, path string) error
	Close() error
}
