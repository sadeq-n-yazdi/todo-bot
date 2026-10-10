package sqlite

import (
	"context"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// All methods below are TODO(#4).

func (d *DB) GetUser(ctx context.Context, id int64) (model.User, error) {
	return model.User{}, errNotImplemented
}
func (d *DB) UpsertUser(ctx context.Context, u model.User) error { return errNotImplemented }
func (d *DB) EnsureChat(ctx context.Context, c model.Chat) error { return errNotImplemented }
func (d *DB) GetChat(ctx context.Context, id int64) (model.Chat, error) {
	return model.Chat{}, errNotImplemented
}
func (d *DB) SetDigest(ctx context.Context, chatID int64, hhmm string, by int64, next *time.Time) error {
	return errNotImplemented
}
func (d *DB) ListChatsByDigestUser(ctx context.Context, userID int64) ([]model.Chat, error) {
	return nil, errNotImplemented
}
func (d *DB) MigrateChat(ctx context.Context, oldID, newID int64) error { return errNotImplemented }
func (d *DB) InsertTodo(ctx context.Context, t *model.Todo) error       { return errNotImplemented }
func (d *DB) GetTodo(ctx context.Context, id int64) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (d *DB) GetTodoByNum(ctx context.Context, chatID int64, num int) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (d *DB) ListTodos(ctx context.Context, q model.TodoQuery) ([]model.Todo, error) {
	return nil, errNotImplemented
}
func (d *DB) CountTodos(ctx context.Context, q model.TodoQuery) (int, error) {
	return 0, errNotImplemented
}
func (d *DB) UpdateTodo(ctx context.Context, t model.Todo) error { return errNotImplemented }
func (d *DB) ListRecurringByCreator(ctx context.Context, userID int64) ([]model.Todo, error) {
	return nil, errNotImplemented
}
func (d *DB) InsertReminder(ctx context.Context, r *model.Reminder) error { return errNotImplemented }
func (d *DB) GetReminder(ctx context.Context, id int64) (model.Reminder, error) {
	return model.Reminder{}, errNotImplemented
}
func (d *DB) UpdateReminder(ctx context.Context, r model.Reminder) error { return errNotImplemented }
func (d *DB) CancelPending(ctx context.Context, todoID int64, kinds ...model.ReminderKind) error {
	return errNotImplemented
}
func (d *DB) DueReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error) {
	return nil, errNotImplemented
}
func (d *DB) NextReminderAt(ctx context.Context) (time.Time, bool, error) {
	return time.Time{}, false, errNotImplemented
}
func (d *DB) DueDigests(ctx context.Context, now time.Time) ([]model.Chat, error) {
	return nil, errNotImplemented
}
func (d *DB) NextDigestAt(ctx context.Context) (time.Time, bool, error) {
	return time.Time{}, false, errNotImplemented
}
func (d *DB) WithTx(ctx context.Context, fn func(tx store.Store) error) error {
	return errNotImplemented
}
func (d *DB) Backup(ctx context.Context, path string) error { return errNotImplemented }
func (d *DB) Close() error                                  { return nil }
