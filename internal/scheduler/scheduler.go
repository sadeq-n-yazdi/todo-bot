// Package scheduler delivers reminders and digests. One goroutine per
// process; the database (through Backend) is the only source of truth, and
// every node may run a scheduler: rows are claimed before delivery, so each
// is handled by one node at a time. See ARCHITECTURE §5 and §9.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Backend is implemented by service.Svc.
type Backend interface {
	// NextWakeup returns min(next claimable reminder, next claimable digest);
	// rows claimed by another node count from their claim_until. ok=false if none.
	NextWakeup(ctx context.Context) (time.Time, bool, error)
	// ClaimReminders claims due reminders for this node (token and claim TTL
	// are the backend's). Claimed rows are this node's to deliver.
	ClaimReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error)
	// ReminderSent marks it sent and, only if that pending->sent transition
	// won, advances a recurring todo and inserts its next 'due' reminder
	// (one transaction). Calling it twice advances once.
	ReminderSent(ctx context.Context, id int64, msgID int, now time.Time) error
	// ReminderFailed bumps attempts and reschedules with backoff, or marks
	// failed; either way it releases the claim. r is the claimed reminder
	// (r.ClaimToken); if the claim was lost meanwhile it is a no-op.
	ReminderFailed(ctx context.Context, r model.Reminder, now time.Time, permanent bool) error
	// CancelReminder cancels a claimed reminder whose todo is no longer open
	// (no-op if the claim was lost).
	CancelReminder(ctx context.Context, r model.Reminder) error
	// ClaimDigests claims chats whose digest is due, like ClaimReminders.
	ClaimDigests(ctx context.Context, now time.Time) ([]model.Chat, error)
	// DigestDone computes and stores the chat's next digest time and clears
	// its claim (c is the claimed chat; no-op if the claim was lost).
	DigestDone(ctx context.Context, c model.Chat, now time.Time) error
	DigestView(ctx context.Context, chatID int64, now time.Time) (model.DigestView, error)
	MigrateChat(ctx context.Context, oldID, newID int64) error
	// Prefs gives the TZ and locale a reminder is rendered in: the todo's
	// creator for an unassigned item; for an assigned item (#58) the
	// assignee for the DM and the creator for the fallback to the creating
	// chat. tgLang is "" here.
	Prefs(ctx context.Context, userID int64, tgLang string) (model.Prefs, error)
	// Housekeep purges expired prompts and old update ids. Run calls it at
	// most once an hour.
	Housekeep(ctx context.Context, now time.Time) error
}

// MaxSleep is the default cap on how long Run sleeps without re-checking.
// With several nodes a lower cap (e.g. 30s) bounds how late a reminder
// created on a node that then died is picked up by another.
const MaxSleep = 5 * time.Minute

// DefaultClaimTTL matches service.Config's default claim TTL.
const DefaultClaimTTL = 2 * time.Minute

// Option configures a Scheduler.
type Option func(*Scheduler)

// WithClaimTTL tells the scheduler how long its claims last (the value given
// to service.Config.ClaimTTL). A 429 RetryAfter is waited out inline only
// while it is shorter than ClaimTTL/2; otherwise the reminder is handed back
// with ReminderFailed(permanent=false). d <= 0 keeps DefaultClaimTTL.
func WithClaimTTL(d time.Duration) Option {
	return func(s *Scheduler) {
		if d > 0 {
			s.claimTTL = d
		}
	}
}

// WithMaxSleep overrides MaxSleep; d <= 0 keeps the default.
func WithMaxSleep(d time.Duration) Option {
	return func(s *Scheduler) {
		if d > 0 {
			s.maxSleep = d
		}
	}
}

// LateAfter is how late a reminder must be to be marked "late".
const LateAfter = 2 * time.Minute

// Scheduler runs the delivery loop.
type Scheduler struct {
	b    Backend
	send tg.Sender
	clk  clock.Clock
	log  *slog.Logger
	wake chan struct{}

	maxSleep time.Duration
	claimTTL time.Duration

	lastHousekeep time.Time
}

// New returns a Scheduler.
func New(b Backend, s tg.Sender, c clock.Clock, log *slog.Logger, opts ...Option) *Scheduler {
	sc := &Scheduler{b: b, send: s, clk: c, log: log, wake: make(chan struct{}, 1), maxSleep: MaxSleep, claimTTL: DefaultClaimTTL}
	for _, o := range opts {
		o(sc)
	}
	return sc
}

// Wake makes Run re-plan now. It never blocks.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
