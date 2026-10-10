// Package scheduler delivers reminders and digests. One goroutine; SQLite
// (through Backend) is the only source of truth. See ARCHITECTURE §5.
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
	// NextWakeup returns min(next pending reminder, next digest); ok=false if none.
	NextWakeup(ctx context.Context) (time.Time, bool, error)
	DueReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error)
	// ReminderSent marks it sent and, for recurring 'due' reminders, advances
	// the todo and inserts the next 'due' reminder (one transaction).
	ReminderSent(ctx context.Context, id int64, msgID int, now time.Time) error
	// ReminderFailed bumps attempts and reschedules with backoff, or marks failed.
	ReminderFailed(ctx context.Context, id int64, now time.Time, permanent bool) error
	// CancelReminder cancels a reminder whose todo is no longer open.
	CancelReminder(ctx context.Context, id int64) error
	DueDigests(ctx context.Context, now time.Time) ([]model.Chat, error)
	// DigestDone computes and stores the chat's next digest time.
	DigestDone(ctx context.Context, chatID int64, now time.Time) error
	DigestView(ctx context.Context, chatID int64, now time.Time) (model.DigestView, error)
	MigrateChat(ctx context.Context, oldID, newID int64) error
	// Prefs gives the TZ and locale a reminder is rendered in (the todo's
	// creator); tgLang is "" here.
	Prefs(ctx context.Context, userID int64, tgLang string) (model.Prefs, error)
}

// MaxSleep caps how long Run sleeps without re-checking.
const MaxSleep = 5 * time.Minute

// LateAfter is how late a reminder must be to be marked "late".
const LateAfter = 2 * time.Minute

// Scheduler runs the delivery loop.
type Scheduler struct {
	b    Backend
	send tg.Sender
	clk  clock.Clock
	log  *slog.Logger
	wake chan struct{}
}

// New returns a Scheduler.
func New(b Backend, s tg.Sender, c clock.Clock, log *slog.Logger) *Scheduler {
	return &Scheduler{b: b, send: s, clk: c, log: log, wake: make(chan struct{}, 1)}
}

// Wake makes Run re-plan now. It never blocks.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run loops until ctx is cancelled. TODO(#10)
func (s *Scheduler) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Tick delivers everything due at clk.Now(). Exported for tests. TODO(#10)
func (s *Scheduler) Tick(ctx context.Context) {}
