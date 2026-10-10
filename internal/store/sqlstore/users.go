package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// GetUser returns the user, or store.ErrNotFound.
func (d *DB) GetUser(ctx context.Context, id int64) (model.User, error) {
	var u model.User
	var lang, cal string
	var created int64
	err := d.q.QueryRowContext(ctx,
		"SELECT id, tz, lang, calendar, created_at FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.TZ, &lang, &cal, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, notFound("user", id)
	}
	if err != nil {
		return model.User{}, fmt.Errorf("sqlstore: get user %d: %w", id, err)
	}
	u.Lang, u.Calendar, u.CreatedAt = i18n.Lang(lang), calendar.Kind(cal), fromUnix(created)
	return u, nil
}

// UpsertUser inserts the user or updates tz, lang and calendar (created_at is kept).
func (d *DB) UpsertUser(ctx context.Context, u model.User) error {
	_, err := d.q.ExecContext(ctx,
		d.d.Upsert("users", []string{"id", "tz", "lang", "calendar", "created_at"}, []string{"id"},
			[]string{"tz", "lang", "calendar"}),
		u.ID, u.TZ, string(u.Lang), string(u.Calendar), unixOrNow(u.CreatedAt))
	if err != nil {
		return fmt.Errorf("sqlstore: upsert user %d: %w", u.ID, err)
	}
	return nil
}
