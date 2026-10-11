package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
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
	minBackoff        = time.Second
	writeTimeout      = 5 * time.Second
)

// Run loops until ctx is cancelled: Tick, then sleep until the next wakeup
// (capped at maxSleep), or until Wake is called. After a failed claim the
// sleep has a floor that doubles up to maxSleep, so a broken database is not
// hammered.
func (s *Scheduler) Run(ctx context.Context) error {
	var backoff time.Duration
	for ctx.Err() == nil {
		failed := s.tick(ctx)
		if ctx.Err() != nil {
			break
		}
		d := s.sleepFor(ctx)
		if failed {
			backoff = min(max(backoff*2, minBackoff), s.maxSleep)
			d = max(d, backoff)
		} else {
			backoff = 0
		}
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
func (s *Scheduler) Tick(ctx context.Context) { s.tick(ctx) }

// tick reports whether a claim call failed.
func (s *Scheduler) tick(ctx context.Context) (claimFailed bool) {
	f1 := s.tickReminders(ctx)
	f2 := s.tickDigests(ctx)
	s.housekeep(ctx)
	return f1 || f2
}

// budgetEnd is the last moment a node may keep waiting on Telegram for rows
// claimed at claimedAt: half the claim TTL, so the claim is still ours.
func (s *Scheduler) budgetEnd(claimedAt time.Time) time.Time {
	return claimedAt.Add(s.claimTTL / 2)
}

// sendMargin is how long before the claim expires every send must be over.
// After the send there is still a database write to make (ReminderSent or
// DigestDone, up to writeTimeout), and the nodes' clocks and the claim_until
// stamp may differ by some seconds. So the margin is a tenth of the claim TTL
// (12s for the default 2m), never less than writeTimeout, and never more than
// a quarter of the TTL so that a very short TTL still leaves time to send.
func (s *Scheduler) sendMargin() time.Duration {
	return min(max(s.claimTTL/10, writeTimeout), s.claimTTL/4)
}

// sendContext bounds the whole send sequence for one claimed item (first
// attempt, 429 waits, migration retries and the assignee fallback) by
// claimedAt + claimTTL - sendMargin. Without it a hung HTTP call can outlive
// the claim and another node would send the same reminder or digest again.
// The budget check between items (budgetEnd) only runs before a send starts.
//
// The timer is a real one on the time left by the injected clock, not a
// clk.After waiter, so a fake clock in tests sees no extra waiters. With the
// real clock the two are the same.
func (s *Scheduler) sendContext(ctx context.Context, claimedAt time.Time) (context.Context, context.CancelFunc) {
	left := claimedAt.Add(s.claimTTL - s.sendMargin()).Sub(s.clk.Now())
	return context.WithTimeout(ctx, max(left, 0))
}

// detached returns a context that survives cancellation of ctx for a short
// time, so a message already sent can still be recorded during shutdown.
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
}

func (s *Scheduler) tickReminders(ctx context.Context) (claimFailed bool) {
	for ctx.Err() == nil {
		claimedAt := s.clk.Now()
		batch, err := s.b.ClaimReminders(ctx, claimedAt, batchSize)
		if err != nil {
			s.log.Error("scheduler: claim reminders", "err", err)
			return true
		}
		end := s.budgetEnd(claimedAt)
		for _, dr := range batch {
			if ctx.Err() != nil {
				return false
			}
			if !s.clk.Now().Before(end) {
				// Out of budget: the rest stay claimed and expire for another node.
				s.log.Warn("scheduler: claim budget spent, leaving rest of batch to expire")
				return false
			}
			s.deliverReminder(ctx, dr, claimedAt)
		}
		if len(batch) < batchSize {
			return false
		}
	}
	return false
}

func (s *Scheduler) deliverReminder(ctx context.Context, dr model.DueReminder, claimedAt time.Time) {
	log := s.log.With("reminder", dr.ID, "todo", dr.TodoID, "chat", dr.ChatID)
	defer func() {
		if p := recover(); p != nil {
			log.Error("scheduler: panic delivering reminder", "panic", p, "stack", string(debug.Stack()))
			s.fail(ctx, dr.Reminder, false)
		}
	}()
	if dr.Todo.Status != model.TodoOpen {
		if err := s.b.CancelReminder(ctx, dr.Reminder); err != nil {
			log.Error("scheduler: cancel reminder", "err", err)
		}
		return
	}
	msgID, ok := s.sendReminder(ctx, log, dr, claimedAt)
	if !ok {
		return
	}
	wctx, cancel := detached(ctx)
	defer cancel()
	if err := s.b.ReminderSent(wctx, dr.ID, msgID, s.clk.Now()); err != nil {
		// The claim will expire and the reminder is sent again (at-least-once).
		log.Error("scheduler: mark reminder sent", "err", err)
	}
}

// sendReminder sends the reminder and returns the id of the message that
// reached a chat. ok=false means the outcome was already recorded
// (ReminderFailed) or the node is shutting down, so the caller must not call
// ReminderSent.
//
// An assigned item (dr.Todo.AssigneeID != 0, ARCHITECTURE §11) goes to the
// assignee's private chat in the assignee's prefs. Only a permanent error
// there (403, chat not found) falls back to the creating chat, in the
// creator's prefs with a mention of the assignee. The fallback is a
// straight-line step after the DM attempt, not a loop, so one claim makes one
// DM attempt (plus its 429 and migration retries) and at most one fallback.
func (s *Scheduler) sendReminder(ctx context.Context, log *slog.Logger, dr model.DueReminder, claimedAt time.Time) (msgID int, ok bool) {
	creator, err := s.b.Prefs(ctx, dr.Todo.CreatedBy, "")
	if err != nil {
		log.Error("scheduler: prefs", "err", err)
		s.fail(ctx, dr.Reminder, false)
		return 0, false
	}
	// One deadline covers the DM and the fallback. Outcomes are recorded
	// with ctx, which this deadline does not cancel.
	sctx, cancel := s.sendContext(ctx, claimedAt)
	defer cancel()
	end := s.budgetEnd(claimedAt)
	now := s.clk.Now()
	late := now.Sub(dr.FireAt) > LateAfter
	assignee := dr.Todo.AssigneeID
	if assignee == 0 || assignee == dr.ChatID {
		id, _, err := s.sendWithRetry(sctx, dr.ChatID, ui.ReminderMsg(dr, creator.Locale, creator.TZ, now, late), end)
		return id, s.sendOutcome(ctx, log, dr, err)
	}

	ap, err := s.b.Prefs(ctx, assignee, "")
	if err != nil {
		log.Error("scheduler: assignee prefs", "assignee", assignee, "err", err)
		s.fail(ctx, dr.Reminder, false)
		return 0, false
	}
	id, _, err := s.sendWithRetry(sctx, assignee, ui.ReminderMsg(dr, ap.Locale, ap.TZ, now, late), end)
	if err == nil {
		return id, true
	}
	if ctx.Err() != nil || sctx.Err() != nil || !tg.IsPermanent(err) {
		// Shutting down, out of time, or transient: normal backoff, no fallback.
		return 0, s.sendOutcome(ctx, log, dr, err)
	}
	log.Warn("scheduler: assignee unreachable, falling back to creating chat", "assignee", assignee, "fallback", true, "err", err)
	fb := ui.ReminderMsg(dr, creator.Locale, creator.TZ, now, late)
	fb.Text = ui.Mention(assignee, dr.Todo.AssigneeName, creator.Locale) + "\n" + fb.Text
	id, _, err = s.sendWithRetry(sctx, dr.ChatID, fb, end)
	return id, s.sendOutcome(ctx, log, dr, err)
}

// sendOutcome reports whether err is nil. Otherwise it records the failure,
// unless the node is shutting down and the claim should simply expire.
func (s *Scheduler) sendOutcome(ctx context.Context, log *slog.Logger, dr model.DueReminder, err error) bool {
	if err == nil {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	log.Warn("scheduler: send reminder", "err", err)
	s.fail(ctx, dr.Reminder, tg.IsPermanent(err))
	return false
}

// sendWithRetry sends msg, waiting out a 429 only while the wait ends before
// end, and retrying after a chat migration. It returns the chat id the message
// finally went to.
func (s *Scheduler) sendWithRetry(ctx context.Context, chatID int64, msg tg.OutMessage, end time.Time) (int, int64, error) {
	var lastErr error
	for range maxSendAttempts {
		id, err := s.send.Send(ctx, chatID, msg)
		if err == nil {
			return id, chatID, nil
		}
		lastErr = err
		var ae *tg.APIError
		if !errors.As(err, &ae) {
			return 0, chatID, err
		}
		switch {
		case ae.MigrateTo != 0:
			if merr := s.b.MigrateChat(ctx, chatID, ae.MigrateTo); merr != nil {
				s.log.Error("scheduler: migrate chat", "old", chatID, "new", ae.MigrateTo, "err", merr)
				return 0, chatID, err
			}
			chatID = ae.MigrateTo
		case ae.Code == 429 && s.clk.Now().Add(ae.RetryAfter).Before(end):
			select {
			case <-ctx.Done():
				return 0, chatID, ctx.Err()
			case <-s.clk.After(ae.RetryAfter):
			}
		default:
			return 0, chatID, err
		}
	}
	return 0, chatID, lastErr
}

func (s *Scheduler) fail(ctx context.Context, r model.Reminder, permanent bool) {
	if ctx.Err() != nil {
		return
	}
	if err := s.b.ReminderFailed(ctx, r, s.clk.Now(), permanent); err != nil {
		s.log.Error("scheduler: mark reminder failed", "reminder", r.ID, "err", err)
	}
}

func (s *Scheduler) tickDigests(ctx context.Context) (claimFailed bool) {
	claimedAt := s.clk.Now()
	chats, err := s.b.ClaimDigests(ctx, claimedAt)
	if err != nil {
		s.log.Error("scheduler: claim digests", "err", err)
		return true
	}
	end := s.budgetEnd(claimedAt)
	for _, c := range chats {
		if ctx.Err() != nil {
			return false
		}
		if !s.clk.Now().Before(end) {
			s.log.Warn("scheduler: claim budget spent, leaving rest of digests to expire")
			return false
		}
		s.deliverDigest(ctx, c, claimedAt)
	}
	return false
}

// deliverDigest sends a chat's digest. DigestDone is called after a success,
// an empty view, a permanent error or an over-1h skip. On a transient error
// the claim is left to expire so any node retries within the lateness window.
func (s *Scheduler) deliverDigest(ctx context.Context, c model.Chat, claimedAt time.Time) {
	log := s.log.With("chat", c.ID)
	done := func() {
		wctx, cancel := detached(ctx)
		defer cancel()
		if err := s.b.DigestDone(wctx, c, s.clk.Now()); err != nil {
			log.Error("scheduler: digest done", "err", err)
		}
	}
	defer func() {
		if p := recover(); p != nil {
			log.Error("scheduler: panic delivering digest", "panic", p, "stack", string(debug.Stack()))
			done()
		}
	}()
	now := s.clk.Now()
	if c.DigestNextAt != nil && now.Sub(*c.DigestNextAt) > digestMaxLate {
		done()
		return
	}
	v, err := s.b.DigestView(ctx, c.ID, now)
	if err != nil {
		log.Error("scheduler: digest view", "err", err)
		return
	}
	if len(v.Overdue) == 0 && len(v.Today) == 0 && v.NoDateCount == 0 {
		done()
		return
	}
	sctx, cancel := s.sendContext(ctx, claimedAt)
	defer cancel()
	_, chatID, err := s.sendWithRetry(sctx, c.ID, ui.Digest(v, now), s.budgetEnd(claimedAt))
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if tg.IsPermanent(err) {
			log.Warn("scheduler: send digest (permanent)", "err", err)
			done()
			return
		}
		log.Warn("scheduler: send digest, will retry", "err", err)
		return
	}
	c.ID = chatID
	done()
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
