package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

const (
	batchSize         = 50
	housekeepInterval = time.Hour
	digestMaxLate     = time.Hour
	maxSendAttempts   = 3
)

// Run loops until ctx is cancelled: Tick, then sleep until the next wakeup
// (capped at maxSleep), or until Wake is called.
func (s *Scheduler) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		s.Tick(ctx)
		if ctx.Err() != nil {
			break
		}
		d := s.sleepFor(ctx)
		select {
		case <-ctx.Done():
		case <-s.wake:
		case <-s.clk.After(d):
		}
	}
	return nil
}

// sleepFor returns clamp(next-now, 0, maxSleep).
func (s *Scheduler) sleepFor(ctx context.Context) time.Duration {
	next, ok, err := s.b.NextWakeup(ctx)
	if err != nil {
		s.log.Error("scheduler: next wakeup", "err", err)
		return s.maxSleep
	}
	if !ok {
		return s.maxSleep
	}
	return min(max(next.Sub(s.clk.Now()), 0), s.maxSleep)
}

// Tick delivers everything due at clk.Now(). Exported for tests.
func (s *Scheduler) Tick(ctx context.Context) {
	s.tickReminders(ctx)
	s.tickDigests(ctx)
	s.housekeep(ctx)
}

func (s *Scheduler) tickReminders(ctx context.Context) {
	for ctx.Err() == nil {
		batch, err := s.b.ClaimReminders(ctx, s.clk.Now(), batchSize)
		if err != nil {
			s.log.Error("scheduler: claim reminders", "err", err)
			return
		}
		for _, dr := range batch {
			if ctx.Err() != nil {
				return
			}
			s.deliverReminder(ctx, dr)
		}
		if len(batch) < batchSize {
			return
		}
	}
}

func (s *Scheduler) deliverReminder(ctx context.Context, dr model.DueReminder) {
	log := s.log.With("reminder", dr.ID, "todo", dr.TodoID, "chat", dr.ChatID)
	if dr.Todo.Status != model.TodoOpen {
		if err := s.b.CancelReminder(ctx, dr.Reminder); err != nil {
			log.Error("scheduler: cancel reminder", "err", err)
		}
		return
	}
	prefs, err := s.b.Prefs(ctx, dr.Todo.CreatedBy, "")
	if err != nil {
		log.Error("scheduler: prefs", "err", err)
		s.fail(ctx, dr.Reminder, false)
		return
	}
	now := s.clk.Now()
	late := now.Sub(dr.FireAt) > LateAfter
	msg := ui.ReminderMsg(dr, prefs.Locale, prefs.TZ, now, late)

	msgID, err := s.sendWithRetry(ctx, &dr, msg)
	if err != nil {
		log.Warn("scheduler: send reminder", "err", err)
		s.fail(ctx, dr.Reminder, tg.IsPermanent(err))
		return
	}
	if err := s.b.ReminderSent(ctx, dr.ID, msgID, s.clk.Now()); err != nil {
		// The claim will expire and the reminder is sent again (at-least-once).
		log.Error("scheduler: mark reminder sent", "err", err)
	}
}

// sendWithRetry sends msg to dr's chat, waiting out a short 429 and retrying
// after a chat migration. dr.ChatID is updated on migration.
func (s *Scheduler) sendWithRetry(ctx context.Context, dr *model.DueReminder, msg tg.OutMessage) (int, error) {
	var lastErr error
	for range maxSendAttempts {
		id, err := s.send.Send(ctx, dr.ChatID, msg)
		if err == nil {
			return id, nil
		}
		lastErr = err
		var ae *tg.APIError
		if !errors.As(err, &ae) {
			return 0, err
		}
		switch {
		case ae.MigrateTo != 0:
			if merr := s.b.MigrateChat(ctx, dr.ChatID, ae.MigrateTo); merr != nil {
				s.log.Error("scheduler: migrate chat", "old", dr.ChatID, "new", ae.MigrateTo, "err", merr)
				return 0, err
			}
			dr.ChatID = ae.MigrateTo
		case ae.Code == 429 && ae.RetryAfter < s.claimTTL/2:
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-s.clk.After(ae.RetryAfter):
			}
		default:
			return 0, err
		}
	}
	return 0, lastErr
}

func (s *Scheduler) fail(ctx context.Context, r model.Reminder, permanent bool) {
	if err := s.b.ReminderFailed(ctx, r, s.clk.Now(), permanent); err != nil {
		s.log.Error("scheduler: mark reminder failed", "reminder", r.ID, "err", err)
	}
}

func (s *Scheduler) tickDigests(ctx context.Context) {
	chats, err := s.b.ClaimDigests(ctx, s.clk.Now())
	if err != nil {
		s.log.Error("scheduler: claim digests", "err", err)
		return
	}
	for _, c := range chats {
		if ctx.Err() != nil {
			return
		}
		s.deliverDigest(ctx, c)
	}
}

func (s *Scheduler) deliverDigest(ctx context.Context, c model.Chat) {
	log := s.log.With("chat", c.ID)
	now := s.clk.Now()
	if c.DigestNextAt == nil || now.Sub(*c.DigestNextAt) <= digestMaxLate {
		s.sendDigest(ctx, c, now, log)
	}
	if err := s.b.DigestDone(ctx, c, s.clk.Now()); err != nil {
		log.Error("scheduler: digest done", "err", err)
	}
}

func (s *Scheduler) sendDigest(ctx context.Context, c model.Chat, now time.Time, log *slog.Logger) {
	v, err := s.b.DigestView(ctx, c.ID, now)
	if err != nil {
		log.Error("scheduler: digest view", "err", err)
		return
	}
	if len(v.Overdue) == 0 && len(v.Today) == 0 && v.NoDateCount == 0 {
		return
	}
	if _, err := s.send.Send(ctx, c.ID, ui.Digest(v, now)); err != nil {
		log.Warn("scheduler: send digest", "err", err)
	}
}

func (s *Scheduler) housekeep(ctx context.Context) {
	now := s.clk.Now()
	if !s.lastHousekeep.IsZero() && now.Sub(s.lastHousekeep) < housekeepInterval {
		return
	}
	s.lastHousekeep = now
	if err := s.b.Housekeep(ctx, now); err != nil {
		s.log.Error("scheduler: housekeep", "err", err)
	}
}
