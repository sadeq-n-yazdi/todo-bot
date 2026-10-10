package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

const todoCols = "id, chat_id, num, text, status, due_at, recur, tz, created_by, created_at, updated_at, done_at"

// todoColsT is todoCols qualified with the "t" alias, for joins.
const todoColsT = "t.id, t.chat_id, t.num, t.text, t.status, t.due_at, t.recur, t.tz, t.created_by, t.created_at, t.updated_at, t.done_at"

// todoDest returns the scan destinations for a todo row plus a finish func
// that completes t from the intermediate nullable values.
func todoDest(t *model.Todo) (dest []any, finish func()) {
	var status string
	var due, done sql.NullInt64
	var recur sql.NullString
	var created, updated int64
	dest = []any{&t.ID, &t.ChatID, &t.Num, &t.Text, &status, &due, &recur, &t.TZ, &t.CreatedBy, &created, &updated, &done}
	finish = func() {
		t.Status, t.DueAt, t.Recur, t.DoneAt = model.TodoStatus(status), ptrTime(due), recur.String, ptrTime(done)
		t.CreatedAt, t.UpdatedAt = fromUnix(created), fromUnix(updated)
	}
	return dest, finish
}

func scanTodo(s scanner) (model.Todo, error) {
	var t model.Todo
	dest, finish := todoDest(&t)
	if err := s.Scan(dest...); err != nil {
		return model.Todo{}, err
	}
	finish()
	return t, nil
}

func (d *DB) queryTodos(ctx context.Context, query string, args ...any) ([]model.Todo, error) {
	rows, err := d.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query todos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.Todo
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan todo: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate todos: %w", err)
	}
	return out, nil
}

// InsertTodo assigns t.ID and the next per-chat t.Num atomically. The chat
// must exist.
func (d *DB) InsertTodo(ctx context.Context, t *model.Todo) error {
	return d.WithTx(ctx, func(tx store.Store) error {
		return tx.(*DB).insertTodo(ctx, t)
	})
}

func (d *DB) insertTodo(ctx context.Context, t *model.Todo) error {
	var num int
	err := d.q.QueryRowContext(ctx,
		"UPDATE chats SET next_num = next_num + 1 WHERE id = ? RETURNING next_num - 1", t.ChatID).Scan(&num)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound("chat", t.ChatID)
	}
	if err != nil {
		return fmt.Errorf("sqlite: allocate todo number in chat %d: %w", t.ChatID, err)
	}
	status := t.Status
	if status == "" {
		status = model.TodoOpen
	}
	created := unixOrNow(t.CreatedAt)
	updated := created
	if !t.UpdatedAt.IsZero() {
		updated = t.UpdatedAt.Unix()
	}
	res, err := d.q.ExecContext(ctx, `
INSERT INTO todos (chat_id, num, text, status, due_at, recur, tz, created_by, created_at, updated_at, done_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ChatID, num, t.Text, string(status), nullTime(t.DueAt), nullString(t.Recur), t.TZ,
		t.CreatedBy, created, updated, nullTime(t.DoneAt))
	if err != nil {
		return fmt.Errorf("sqlite: insert todo: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("sqlite: insert todo: %w", err)
	}
	t.ID, t.Num, t.Status = id, num, status
	t.CreatedAt, t.UpdatedAt = fromUnix(created), fromUnix(updated)
	return nil
}

// GetTodo returns the todo, or store.ErrNotFound.
func (d *DB) GetTodo(ctx context.Context, id int64) (model.Todo, error) {
	t, err := scanTodo(d.q.QueryRowContext(ctx, "SELECT "+todoCols+" FROM todos WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Todo{}, notFound("todo", id)
	}
	if err != nil {
		return model.Todo{}, fmt.Errorf("sqlite: get todo %d: %w", id, err)
	}
	return t, nil
}

// GetTodoByNum returns the todo with the per-chat number, or store.ErrNotFound.
func (d *DB) GetTodoByNum(ctx context.Context, chatID int64, num int) (model.Todo, error) {
	t, err := scanTodo(d.q.QueryRowContext(ctx,
		"SELECT "+todoCols+" FROM todos WHERE chat_id = ? AND num = ?", chatID, num))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Todo{}, notFound("todo", fmt.Sprintf("%d#%d", chatID, num))
	}
	if err != nil {
		return model.Todo{}, fmt.Errorf("sqlite: get todo %d#%d: %w", chatID, num, err)
	}
	return t, nil
}

// where builds the WHERE clause for q. DueBefore matches todos with a due
// time strictly before it; undated todos never match.
func where(q model.TodoQuery) (string, []any) {
	var conds []string
	var args []any
	if q.ChatID != 0 {
		conds = append(conds, "chat_id = ?")
		args = append(args, q.ChatID)
	}
	if len(q.Statuses) > 0 {
		conds = append(conds, "status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(q.Statuses)), ",")+")")
		for _, s := range q.Statuses {
			args = append(args, string(s))
		}
	}
	if q.DueBefore != nil {
		conds = append(conds, "due_at IS NOT NULL AND due_at < ?")
		args = append(args, q.DueBefore.Unix())
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListTodos orders by (due_at IS NULL, due_at, num).
func (d *DB) ListTodos(ctx context.Context, q model.TodoQuery) ([]model.Todo, error) {
	w, args := where(q)
	query := "SELECT " + todoCols + " FROM todos" + w + " ORDER BY (due_at IS NULL), due_at, num"
	if q.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, q.Limit, q.Offset)
	} else if q.Offset > 0 {
		query += " LIMIT -1 OFFSET ?"
		args = append(args, q.Offset)
	}
	return d.queryTodos(ctx, query, args...)
}

// CountTodos counts the todos matching q, ignoring Limit and Offset.
func (d *DB) CountTodos(ctx context.Context, q model.TodoQuery) (int, error) {
	w, args := where(q)
	var n int
	if err := d.q.QueryRowContext(ctx, "SELECT COUNT(*) FROM todos"+w, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: count todos: %w", err)
	}
	return n, nil
}

// UpdateTodo rewrites every mutable column of the todo with t.ID.
func (d *DB) UpdateTodo(ctx context.Context, t model.Todo) error {
	res, err := d.q.ExecContext(ctx, `
UPDATE todos SET text = ?, status = ?, due_at = ?, recur = ?, tz = ?, updated_at = ?, done_at = ?
WHERE id = ?`,
		t.Text, string(t.Status), nullTime(t.DueAt), nullString(t.Recur), t.TZ,
		unixOrNow(t.UpdatedAt), nullTime(t.DoneAt), t.ID)
	return checkAffected(res, err, "update", "todo", t.ID)
}

// ListRecurringByCreator returns open recurring todos created by userID.
func (d *DB) ListRecurringByCreator(ctx context.Context, userID int64) ([]model.Todo, error) {
	return d.queryTodos(ctx, "SELECT "+todoCols+
		" FROM todos WHERE created_by = ? AND recur IS NOT NULL AND status = 'open' ORDER BY chat_id, num", userID)
}
