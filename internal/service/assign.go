package service

import (
	"context"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Assignment (#58, ARCHITECTURE §11). AddTodoFor lives in todos.go next to
// AddTodo, which delegates to it; the authorisation rule is in ownedTodo.

// normUsername lowercases a Telegram username and drops a leading "@".
func normUsername(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))
}

// NoteUsername implements Service. It compares with the database row, never
// a cache, so any node can serve any update. SetUsername runs on the pool,
// not inside WithTx, so concurrent nodes cannot make it fail (§11).
func (s *Svc) NoteUsername(ctx context.Context, userID int64, username string) error {
	u, err := s.ensureUser(ctx, s.st, userID, "")
	if err != nil {
		return err
	}
	name := normUsername(username)
	if u.Username == name {
		return nil
	}
	return s.st.SetUsername(ctx, userID, name)
}

// UserByUsername implements Service.
func (s *Svc) UserByUsername(ctx context.Context, username string) (model.User, error) {
	return s.st.UserByUsername(ctx, normUsername(username))
}

// ListAssigned implements Service. Items in the user's own private chat are
// left out: they already show in the normal list.
func (s *Svc) ListAssigned(ctx context.Context, userID int64) ([]model.Todo, error) {
	all, err := s.st.ListAssigned(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.ChatID != userID {
			out = append(out, t)
		}
	}
	return out, nil
}
