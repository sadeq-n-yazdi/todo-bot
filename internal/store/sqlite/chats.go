package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

const chatCols = "id, kind, title, digest_time, digest_by, digest_next_at, created_at"

func scanChat(s scanner) (model.Chat, error) {
	var c model.Chat
	var kind string
	var dtime sql.NullString
	var by, next sql.NullInt64
	var created int64
	if err := s.Scan(&c.ID, &kind, &c.Title, &dtime, &by, &next, &created); err != nil {
		return model.Chat{}, err
	}
	c.Kind, c.DigestTime, c.DigestBy = model.ChatKind(kind), dtime.String, by.Int64
	c.DigestNextAt, c.CreatedAt = ptrTime(next), fromUnix(created)
	return c, nil
}

func (d *DB) queryChats(ctx context.Context, query string, args ...any) ([]model.Chat, error) {
	rows, err := d.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query chats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan chat: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate chats: %w", err)
	}
	return out, nil
}

// EnsureChat inserts the chat if missing, or updates its title.
func (d *DB) EnsureChat(ctx context.Context, c model.Chat) error {
	_, err := d.q.ExecContext(ctx, `
INSERT INTO chats (id, kind, title, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET title = excluded.title`,
		c.ID, string(c.Kind), c.Title, unixOrNow(c.CreatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: ensure chat %d: %w", c.ID, err)
	}
	return nil
}

// GetChat returns the chat, or store.ErrNotFound.
func (d *DB) GetChat(ctx context.Context, id int64) (model.Chat, error) {
	c, err := scanChat(d.q.QueryRowContext(ctx, "SELECT "+chatCols+" FROM chats WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Chat{}, notFound("chat", id)
	}
	if err != nil {
		return model.Chat{}, fmt.Errorf("sqlite: get chat %d: %w", id, err)
	}
	return c, nil
}

// SetDigest sets digest_time/digest_by/digest_next_at; hhmm "" and next nil turn it off.
func (d *DB) SetDigest(ctx context.Context, chatID int64, hhmm string, by int64, next *time.Time) error {
	res, err := d.q.ExecContext(ctx,
		"UPDATE chats SET digest_time = ?, digest_by = ?, digest_next_at = ? WHERE id = ?",
		nullString(hhmm), nullInt(by), nullTime(next), chatID)
	return checkAffected(res, err, "set digest", "chat", chatID)
}

// ListChatsByDigestUser returns chats whose digest was set by userID.
func (d *DB) ListChatsByDigestUser(ctx context.Context, userID int64) ([]model.Chat, error) {
	return d.queryChats(ctx,
		"SELECT "+chatCols+" FROM chats WHERE digest_by = ? AND digest_time IS NOT NULL ORDER BY id", userID)
}

// DueDigests returns chats with digest_next_at <= now.
func (d *DB) DueDigests(ctx context.Context, now time.Time) ([]model.Chat, error) {
	return d.queryChats(ctx,
		"SELECT "+chatCols+" FROM chats WHERE digest_next_at IS NOT NULL AND digest_next_at <= ? ORDER BY digest_next_at, id",
		now.Unix())
}

// NextDigestAt returns the earliest digest_next_at, ok=false if none.
func (d *DB) NextDigestAt(ctx context.Context) (time.Time, bool, error) {
	return d.minTime(ctx, "SELECT MIN(digest_next_at) FROM chats WHERE digest_next_at IS NOT NULL")
}

// MigrateChat moves a chat and all its todos and reminders to newID in one
// transaction. If newID already exists, the todos and reminders are merged
// into it and the old chat row is deleted; todo numbers that clash with the
// target get fresh numbers from the target's next_num.
func (d *DB) MigrateChat(ctx context.Context, oldID, newID int64) error {
	if oldID == newID {
		return nil
	}
	return d.WithTx(ctx, func(tx store.Store) error {
		return tx.(*DB).migrateChat(ctx, oldID, newID)
	})
}

type todoMove struct{ id, num int64 }

func (d *DB) migrateChat(ctx context.Context, oldID, newID int64) error {
	var oldNext int64
	err := d.q.QueryRowContext(ctx, "SELECT next_num FROM chats WHERE id = ?", oldID).Scan(&oldNext)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("chat", oldID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: migrate chat %d: %w", oldID, err)
	}
	var targetNext int64
	err = d.q.QueryRowContext(ctx, "SELECT next_num FROM chats WHERE id = ?", newID).Scan(&targetNext)
	if errors.Is(err, sql.ErrNoRows) {
		// Plain rename; chats.id cascades to todos.chat_id.
		if _, err := d.q.ExecContext(ctx, "UPDATE chats SET id = ? WHERE id = ?", newID, oldID); err != nil {
			return fmt.Errorf("sqlite: rename chat %d: %w", oldID, err)
		}
		return d.moveReminders(ctx, oldID, newID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: migrate chat %d: %w", newID, err)
	}

	// Merge into the existing target. Fresh numbers start above both chats'
	// counters so they cannot clash with any kept number.
	next := max(targetNext, oldNext)
	moves, err := d.planTodoMoves(ctx, oldID, newID, &next)
	if err != nil {
		return err
	}
	for _, m := range moves {
		if _, err := d.q.ExecContext(ctx, "UPDATE todos SET chat_id = ?, num = ? WHERE id = ?", newID, m.num, m.id); err != nil {
			return fmt.Errorf("sqlite: move todo %d: %w", m.id, err)
		}
	}
	if _, err := d.q.ExecContext(ctx, "UPDATE chats SET next_num = ? WHERE id = ?", next, newID); err != nil {
		return fmt.Errorf("sqlite: update next_num of chat %d: %w", newID, err)
	}
	if err := d.moveReminders(ctx, oldID, newID); err != nil {
		return err
	}
	if _, err := d.q.ExecContext(ctx, "DELETE FROM chats WHERE id = ?", oldID); err != nil {
		return fmt.Errorf("sqlite: delete chat %d: %w", oldID, err)
	}
	return nil
}

// planTodoMoves decides the new num of every todo of oldID. Clashing nums are
// taken from *next, which is advanced past every num in use.
func (d *DB) planTodoMoves(ctx context.Context, oldID, newID int64, next *int64) ([]todoMove, error) {
	rows, err := d.q.QueryContext(ctx, `
SELECT id, num, num IN (SELECT num FROM todos WHERE chat_id = ?) FROM todos WHERE chat_id = ? ORDER BY num`,
		newID, oldID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: merge chat %d: %w", oldID, err)
	}
	defer func() { _ = rows.Close() }()
	var moves []todoMove
	for rows.Next() {
		var id, num int64
		var clash bool
		if err := rows.Scan(&id, &num, &clash); err != nil {
			return nil, fmt.Errorf("sqlite: merge chat %d: %w", oldID, err)
		}
		if clash {
			num = *next
			*next++
		}
		moves = append(moves, todoMove{id, num})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: merge chat %d: %w", oldID, err)
	}
	for _, m := range moves {
		*next = max(*next, m.num+1)
	}
	return moves, nil
}

func (d *DB) moveReminders(ctx context.Context, oldID, newID int64) error {
	if _, err := d.q.ExecContext(ctx, "UPDATE reminders SET chat_id = ? WHERE chat_id = ?", newID, oldID); err != nil {
		return fmt.Errorf("sqlite: move reminders of chat %d: %w", oldID, err)
	}
	return nil
}
