package service

import (
	"context"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Task-creation wizard drafts (#68, ARCHITECTURE §12). These are thin
// wrappers over the store's compare-and-set draft methods; they add the
// clock and DraftTTL. The wizard's flow lives in the handler.

// StartDraft implements Service.
func (s *Svc) StartDraft(ctx context.Context, d *model.Draft) error {
	now := s.clk.Now().UTC()
	d.UpdatedAt, d.ExpiresAt = now, now.Add(s.cfg.DraftTTL)
	return s.st.PutDraft(ctx, d)
}

// Draft implements Service.
func (s *Svc) Draft(ctx context.Context, chatID, userID int64) (model.Draft, error) {
	return s.st.GetDraft(ctx, chatID, userID, s.clk.Now().UTC())
}

// DraftByMessage implements Service.
func (s *Svc) DraftByMessage(ctx context.Context, chatID int64, msgID int) (model.Draft, error) {
	return s.st.DraftByMessage(ctx, chatID, msgID, s.clk.Now().UTC())
}

// UpdateDraft implements Service. d is changed only on success.
func (s *Svc) UpdateDraft(ctx context.Context, d *model.Draft) error {
	now := s.clk.Now().UTC()
	c := *d
	c.UpdatedAt, c.ExpiresAt = now, now.Add(s.cfg.DraftTTL)
	ok, err := s.st.UpdateDraft(ctx, &c, now)
	if err != nil {
		return err
	}
	if !ok {
		return ErrStaleDraft
	}
	*d = c
	return nil
}

// EndDraft implements Service.
func (s *Svc) EndDraft(ctx context.Context, d model.Draft) error {
	ok, err := s.st.DeleteDraft(ctx, d.ChatID, d.UserID, d.Version, s.clk.Now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return ErrStaleDraft
	}
	return nil
}

// SaveDraft implements Service: the conditional delete and the insert are
// one transaction, so either both happen or neither does.
func (s *Svc) SaveDraft(ctx context.Context, d model.Draft, a Actor, assigneeID int64, r nlparse.Result) (model.Todo, error) {
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
		out, err = s.addTodoTx(ctx, tx, a, assigneeID, r, now)
		return err
	})
	if err != nil {
		return model.Todo{}, err
	}
	if out.DueAt != nil {
		s.wake()
	}
	return out, nil
}

// SetKBVersion implements Service.
func (s *Svc) SetKBVersion(ctx context.Context, userID int64, v int) error {
	return s.st.WithTx(ctx, func(tx store.Store) error {
		if _, err := s.ensureUser(ctx, tx, userID, ""); err != nil {
			return err
		}
		return tx.SetKBVersion(ctx, userID, v)
	})
}

// NamedUsers implements Service.
func (s *Svc) NamedUsers(ctx context.Context, exceptID int64) ([]model.User, error) {
	all, err := s.st.ListNamedUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, u := range all {
		if u.ID != exceptID {
			out = append(out, u)
		}
	}
	return out, nil
}
