package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

const userCols = "id, tz, lang, calendar, COALESCE(username, ''), created_at"

func scanUser(s scanner) (model.User, error) {
	var u model.User
	var lang, cal string
	var created int64
	if err := s.Scan(&u.ID, &u.TZ, &lang, &cal, &u.Username, &created); err != nil {
		return model.User{}, err
	}
	u.Lang, u.Calendar, u.CreatedAt = i18n.Lang(lang), calendar.Kind(cal), fromUnix(created)
	return u, nil
}

// GetUser returns the user, or store.ErrNotFound.
func (d *DB) GetUser(ctx context.Context, id int64) (model.User, error) {
	u, err := scanUser(d.q.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, notFound("user", id)
	}
	if err != nil {
		return model.User{}, fmt.Errorf("sqlstore: get user %d: %w", id, err)
	}
	return u, nil
}

// UpsertUser inserts the user or updates tz, lang and calendar (created_at
// and username are kept).
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

// normUsername lowercases a Telegram username and drops a leading "@".
func normUsername(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))
}

// SetUsername stores the user's username and, in the same transaction,
// clears it from whoever had it before. "" clears the user's name.
func (d *DB) SetUsername(ctx context.Context, userID int64, name string) error {
	name = normUsername(name)
	return d.WithTx(ctx, func(tx store.Store) error {
		q := tx.(*DB).q
		if name != "" {
			if _, err := q.ExecContext(ctx,
				"UPDATE users SET username = NULL WHERE username = ? AND id <> ?", name, userID); err != nil {
				return fmt.Errorf("sqlstore: release username %q: %w", name, err)
			}
		}
		res, err := q.ExecContext(ctx, "UPDATE users SET username = ? WHERE id = ?", nullString(name), userID)
		return checkAffected(res, err, "set username of user", "user", userID)
	})
}

// UserByUsername returns the user with the username, or store.ErrNotFound.
func (d *DB) UserByUsername(ctx context.Context, name string) (model.User, error) {
	name = normUsername(name)
	if name == "" {
		return model.User{}, notFound("username", `""`)
	}
	u, err := scanUser(d.q.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE username = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, notFound("username", name)
	}
	if err != nil {
		return model.User{}, fmt.Errorf("sqlstore: user by username %q: %w", name, err)
	}
	return u, nil
}

// ListNamedUsers returns the users with a known username, by username.
func (d *DB) ListNamedUsers(ctx context.Context) ([]model.User, error) {
	rows, err := d.q.QueryContext(ctx,
		"SELECT "+userCols+" FROM users WHERE username IS NOT NULL ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("sqlstore: list named users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlstore: list named users: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlstore: list named users: %w", err)
	}
	return out, nil
}
