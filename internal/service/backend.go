package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// digestBatch bounds how many digests one ClaimDigests call takes.
const digestBatch = 50

// claimToken returns a fresh NodeID:random token; every claim call uses its
// own, so a node whose claim expired cannot touch rows someone else claimed.
func (s *Svc) claimToken() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("service: claim token: %w", err)
	}
	return s.cfg.NodeID + ":" + hex.EncodeToString(b[:]), nil
}

// NextWakeup implements scheduler.Backend. Rows claimed by another node
// count from their claim expiry (the store's rule).
func (s *Svc) NextWakeup(ctx context.Context) (time.Time, bool, error) {
	rt, rok, err := s.st.NextReminderAt(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	dt, dok, err := s.st.NextDigestAt(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	switch {
	case rok && dok:
		return minTime(rt, dt), true, nil
	case rok:
		return rt, true, nil
	case dok:
		return dt, true, nil
	}
	return time.Time{}, false, nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// ClaimReminders implements scheduler.Backend.
func (s *Svc) ClaimReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error) {
	tok, err := s.claimToken()
	if err != nil {
		return nil, err
	}
	return s.st.ClaimReminders(ctx, tok, now.UTC(), now.UTC().Add(s.cfg.ClaimTTL), limit)
}

// ReminderSent implements scheduler.Backend. Only the caller that wins the
// pending->sent transition advances a recurring todo, in the same
// transaction, so a second call (or a second node) changes nothing.
func (s *Svc) ReminderSent(ctx context.Context, id int64, msgID int, now time.Time) error {
	now = now.UTC()
	advanced := false
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		r, err := tx.GetReminder(ctx, id)
		if err != nil {
			return err
		}
		ok, err := tx.MarkReminderSent(ctx, id, msgID, now)
		if err != nil || !ok {
			return err
		}
		if r.Kind != model.ReminderDue {
			return nil
		}
		t, err := tx.GetTodo(ctx, r.TodoID)
		if err != nil {
			return err
		}
		if t.Status != model.TodoOpen || t.Recur == "" {
			return nil
		}
		advanced, err = s.advanceRecurring(ctx, tx, &t, laterOf(now, r.FireAt), now)
		return err
	})
	if err != nil {
		return err
	}
	if advanced {
		s.wake()
	}
	return nil
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// ReminderFailed implements scheduler.Backend. It counts the failure and
// either reschedules the reminder at now+delay (the policy is s.retry) or
// marks it failed, releasing the claim in both cases. The write is
// conditional on r.ClaimToken: if the claim was lost it does nothing. When a
// recurring series' reminder is given up on after transient errors, the
// series moves on to its next occurrence instead of stopping.
func (s *Svc) ReminderFailed(ctx context.Context, r model.Reminder, now time.Time, permanent bool) error {
	now = now.UTC()
	r.Attempts++
	giveUp := permanent
	if !permanent {
		var delay time.Duration
		delay, giveUp = s.retry(r.Attempts)
		if !giveUp {
			r.FireAt = now.Add(max(delay, 0))
		}
	}
	if giveUp {
		r.State = model.ReminderFailed
	} else {
		r.State = model.ReminderPending
	}
	advanced := false
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		ok, err := tx.RescheduleClaimed(ctx, r)
		if err != nil {
			return err
		}
		if !ok {
			s.log.Debug("service: reminder claim lost on failure", "reminder", r.ID)
			return nil
		}
		if !giveUp {
			advanced = true // new fire time
			return nil
		}
		if permanent || r.Kind != model.ReminderDue {
			return nil
		}
		t, err := tx.GetTodo(ctx, r.TodoID)
		if err != nil {
			return err
		}
		if t.Status != model.TodoOpen || t.Recur == "" {
			return nil
		}
		advanced, err = s.advanceRecurring(ctx, tx, &t, laterOf(now, r.FireAt), now)
		return err
	})
	if err != nil {
		return err
	}
	if advanced {
		s.wake()
	}
	return nil
}

// CancelReminder implements scheduler.Backend.
func (s *Svc) CancelReminder(ctx context.Context, r model.Reminder) error {
	r.State = model.ReminderCancelled
	ok, err := s.st.RescheduleClaimed(ctx, r)
	if err != nil {
		return err
	}
	if !ok {
		s.log.Debug("service: reminder claim lost on cancel", "reminder", r.ID)
	}
	return nil
}

// ClaimDigests implements scheduler.Backend.
func (s *Svc) ClaimDigests(ctx context.Context, now time.Time) ([]model.Chat, error) {
	tok, err := s.claimToken()
	if err != nil {
		return nil, err
	}
	return s.st.ClaimDigests(ctx, tok, now.UTC(), now.UTC().Add(s.cfg.ClaimTTL), digestBatch)
}

// Housekeep implements scheduler.Backend: it purges expired prompts and old
// update ids.
func (s *Svc) Housekeep(ctx context.Context, now time.Time) error {
	return s.st.Purge(ctx, now.UTC())
}

// MigrateChat implements Service. A second node reporting the same
// migration finds the old chat gone and the new one present, which is fine.
func (s *Svc) MigrateChat(ctx context.Context, oldID, newID int64) error {
	err := s.st.MigrateChat(ctx, oldID, newID)
	if errors.Is(err, store.ErrNotFound) {
		if _, gerr := s.st.GetChat(ctx, newID); gerr == nil {
			return nil
		}
	}
	return err
}

// PutPrompt implements Service.
func (s *Svc) PutPrompt(ctx context.Context, p model.Prompt) error {
	p.ExpiresAt = s.clk.Now().UTC().Add(s.cfg.PromptTTL)
	return s.st.PutPrompt(ctx, p)
}

// TakePrompt implements Service.
func (s *Svc) TakePrompt(ctx context.Context, chatID int64, msgID int, userID int64) (model.Prompt, error) {
	return s.st.TakePrompt(ctx, chatID, msgID, userID, s.clk.Now().UTC())
}

// MarkUpdate implements Service.
func (s *Svc) MarkUpdate(ctx context.Context, updateID int64) (bool, error) {
	return s.st.MarkUpdate(ctx, updateID, s.clk.Now().UTC())
}
