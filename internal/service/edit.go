package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Editing an item with the wizard (#98, ARCHITECTURE §13).

// UpdateTodo implements Service.
func (s *Svc) UpdateTodo(ctx context.Context, a Actor, todoID int64, p TodoPatch) (model.Todo, error) {
	now := s.clk.Now().UTC()
	var out model.Todo
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		var err error
		out, err = s.updateTodoTx(ctx, tx, a, todoID, p, now)
		return err
	})
	if err != nil {
		return model.Todo{}, err
	}
	if p.When != nil {
		s.wake()
	}
	return out, nil
}

// SaveEditDraft implements Service: the conditional delete of the draft and
// the update are one transaction, so either both happen or neither does.
func (s *Svc) SaveEditDraft(ctx context.Context, d model.Draft, a Actor, todoID int64, p TodoPatch) (model.Todo, error) {
	now := s.clk.Now().UTC()
	var out model.Todo
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		ok, err := tx.DeleteDraft(ctx, d.ChatID, d.UserID, d.Version, now)
		if err != nil {
			return err
		}
		if !ok {
			return ErrStaleDraft
		}
		out, err = s.updateTodoTx(ctx, tx, a, todoID, p, now)
		return err
	})
	if err != nil {
		return model.Todo{}, err
	}
	if p.When != nil {
		s.wake()
	}
	return out, nil
}

// updateTodoTx is the body of UpdateTodo, inside the caller's transaction.
// It reads the item again, so it never writes back a stale copy.
func (s *Svc) updateTodoTx(ctx context.Context, tx store.Store, a Actor, todoID int64, p TodoPatch, now time.Time) (model.Todo, error) {
	t, err := ownedTodo(ctx, tx, a, todoID)
	if err != nil {
		return model.Todo{}, err
	}
	if t.Status != model.TodoOpen {
		return model.Todo{}, ErrNotFound
	}
	if p.Text != nil {
		text := strings.TrimSpace(*p.Text)
		if text == "" {
			return model.Todo{}, ErrEmptyText
		}
		t.Text = text
	}
	if p.AssigneeID != nil {
		id := *p.AssigneeID
		if id == t.ChatID {
			id = 0 // the assignee's own private chat
		}
		if id != 0 {
			if _, err := tx.GetUser(ctx, id); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return model.Todo{}, ErrUnknownAssignee
				}
				return model.Todo{}, err
			}
		}
		t.AssigneeID = id
	}
	var newDue *time.Time
	if p.When != nil {
		u, err := s.ensureUser(ctx, tx, a.UserID, "")
		if err != nil {
			return model.Todo{}, err
		}
		loc := s.locOrDefault(u.TZ)
		r := *p.When
		t.DueAt, t.Recur, t.TZ = nil, "", u.TZ
		if r.Due != nil {
			d := r.Due.UTC()
			t.DueAt = &d
		}
		if r.Rule != nil {
			t.Recur = r.Rule.String()
			if t.DueAt == nil || !t.DueAt.After(now) {
				next := r.Rule.Next(now, loc)
				if next.IsZero() {
					return model.Todo{}, fmt.Errorf("service: recurrence %q has no next occurrence", t.Recur)
				}
				next = next.UTC()
				t.DueAt = &next
			}
		}
		if t.DueAt != nil && t.DueAt.After(now) && (r.Rule != nil || !dateOnlySentinel(r, loc, now)) {
			newDue = t.DueAt
		}
	}
	t.UpdatedAt = now
	if err := tx.UpdateTodo(ctx, t); err != nil {
		return model.Todo{}, err
	}
	if p.When != nil {
		if err := tx.CancelPending(ctx, t.ID, model.ReminderDue, model.ReminderSnooze); err != nil {
			return model.Todo{}, err
		}
		if newDue != nil {
			if err := tx.InsertReminder(ctx, newReminder(t, *newDue, model.ReminderDue, now)); err != nil {
				return model.Todo{}, err
			}
		}
	}
	t.AssigneeName = ""
	if t.AssigneeID != 0 {
		as, err := tx.GetUser(ctx, t.AssigneeID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return model.Todo{}, err
		}
		t.AssigneeName = as.Username
	}
	return t, nil
}
