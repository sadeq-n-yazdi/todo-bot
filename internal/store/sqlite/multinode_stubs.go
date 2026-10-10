package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Multi-node methods from the v2 contract. TODO(#25): implemented in sqlstore.

var errNotImplemented = errors.New("sqlite: not implemented")

var (
	_ store.Backuper = (*DB)(nil)
	_ store.Migrator = (*DB)(nil)
)

func (d *DB) ClaimReminders(ctx context.Context, token string, now, until time.Time, limit int) ([]model.DueReminder, error) {
	return nil, errNotImplemented
}
func (d *DB) MarkReminderSent(ctx context.Context, id int64, msgID int, at time.Time) (bool, error) {
	return false, errNotImplemented
}
func (d *DB) ClaimDigests(ctx context.Context, token string, now, until time.Time, limit int) ([]model.Chat, error) {
	return nil, errNotImplemented
}
func (d *DB) AcquireLease(ctx context.Context, name, holder string, now time.Time, ttl time.Duration) (model.Lease, error) {
	return model.Lease{}, errNotImplemented
}
func (d *DB) ReleaseLease(ctx context.Context, name, holder string) error { return errNotImplemented }
func (d *DB) PutPrompt(ctx context.Context, p model.Prompt) error         { return errNotImplemented }
func (d *DB) TakePrompt(ctx context.Context, chatID int64, msgID int, now time.Time) (model.Prompt, error) {
	return model.Prompt{}, errNotImplemented
}
func (d *DB) MarkUpdate(ctx context.Context, updateID int64, now time.Time) (bool, error) {
	return false, errNotImplemented
}
func (d *DB) Purge(ctx context.Context, now time.Time) error { return errNotImplemented }
