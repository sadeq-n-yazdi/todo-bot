package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

const reminderCols = "id, todo_id, chat_id, fire_at, kind, state, attempts, sent_at, message_id, created_at"

// reminderColsR is reminderCols qualified with the "r" alias, for joins.
const reminderColsR = "r.id, r.todo_id, r.chat_id, r.fire_at, r.kind, r.state, r.attempts, r.sent_at, r.message_id, r.created_at"

func reminderDest(r *model.Reminder) (dest []any, finish func()) {
	var fire, created int64
	var kind, state string
	var sent, msg sql.NullInt64
	dest = []any{&r.ID, &r.TodoID, &r.ChatID, &fire, &kind, &state, &r.Attempts, &sent, &msg, &created}
	finish = func() {
		r.FireAt, r.Kind, r.State = fromUnix(fire), model.ReminderKind(kind), model.ReminderState(state)
		r.SentAt, r.MessageID, r.CreatedAt = ptrTime(sent), int(msg.Int64), fromUnix(created)
	}
	return dest, finish
}

func scanReminder(s scanner) (model.Reminder, error) {
	var r model.Reminder
	dest, finish := reminderDest(&r)
	if err := s.Scan(dest...); err != nil {
		return model.Reminder{}, err
	}
	finish()
	return r, nil
}

// InsertReminder assigns r.ID. A second pending due reminder for the same
// todo violates the partial unique index and is returned as an error.
func (d *DB) InsertReminder(ctx context.Context, r *model.Reminder) error {
	state := r.State
	if state == "" {
		state = model.ReminderPending
	}
	created := unixOrNow(r.CreatedAt)
	res, err := d.q.ExecContext(ctx, `
INSERT INTO reminders (todo_id, chat_id, fire_at, kind, state, attempts, sent_at, message_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TodoID, r.ChatID, r.FireAt.Unix(), string(r.Kind), string(state), r.Attempts,
		nullTime(r.SentAt), nullInt(int64(r.MessageID)), created)
	if err != nil {
		return fmt.Errorf("sqlite: insert reminder: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("sqlite: insert reminder: %w", err)
	}
	r.ID, r.State, r.CreatedAt = id, state, fromUnix(created)
	return nil
}

// GetReminder returns the reminder, or store.ErrNotFound.
func (d *DB) GetReminder(ctx context.Context, id int64) (model.Reminder, error) {
	r, err := scanReminder(d.q.QueryRowContext(ctx, "SELECT "+reminderCols+" FROM reminders WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Reminder{}, notFound("reminder", id)
	}
	if err != nil {
		return model.Reminder{}, fmt.Errorf("sqlite: get reminder %d: %w", id, err)
	}
	return r, nil
}

// UpdateReminder rewrites every mutable column of the reminder with r.ID.
func (d *DB) UpdateReminder(ctx context.Context, r model.Reminder) error {
	res, err := d.q.ExecContext(ctx, `
UPDATE reminders SET fire_at = ?, kind = ?, state = ?, attempts = ?, sent_at = ?, message_id = ?
WHERE id = ?`,
		r.FireAt.Unix(), string(r.Kind), string(r.State), r.Attempts,
		nullTime(r.SentAt), nullInt(int64(r.MessageID)), r.ID)
	return checkAffected(res, err, "update", "reminder", r.ID)
}

// CancelPending cancels pending reminders of a todo; no kinds = all kinds.
func (d *DB) CancelPending(ctx context.Context, todoID int64, kinds ...model.ReminderKind) error {
	query := "UPDATE reminders SET state = 'cancelled' WHERE todo_id = ? AND state = 'pending'"
	args := []any{todoID}
	if len(kinds) > 0 {
		query += " AND kind IN (" + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + ")"
		for _, k := range kinds {
			args = append(args, string(k))
		}
	}
	if _, err := d.q.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("sqlite: cancel pending reminders of todo %d: %w", todoID, err)
	}
	return nil
}

// DueReminders returns pending reminders with fire_at <= now, oldest first,
// each joined with its todo.
func (d *DB) DueReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error) {
	query := "SELECT " + reminderColsR + ", " + todoColsT + ` FROM reminders r JOIN todos t ON t.id = r.todo_id
WHERE r.state = 'pending' AND r.fire_at <= ? ORDER BY r.fire_at, r.id`
	args := []any{now.Unix()}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := d.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query due reminders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.DueReminder
	for rows.Next() {
		var dr model.DueReminder
		rd, rfinish := reminderDest(&dr.Reminder)
		td, tfinish := todoDest(&dr.Todo)
		if err := rows.Scan(append(rd, td...)...); err != nil {
			return nil, fmt.Errorf("sqlite: scan due reminder: %w", err)
		}
		rfinish()
		tfinish()
		out = append(out, dr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate due reminders: %w", err)
	}
	return out, nil
}

// NextReminderAt returns the earliest pending fire_at, ok=false if none.
func (d *DB) NextReminderAt(ctx context.Context) (time.Time, bool, error) {
	return d.minTime(ctx, "SELECT MIN(fire_at) FROM reminders WHERE state = 'pending'")
}
