// Package service holds the business logic on top of store.Store. It also
// implements scheduler.Backend.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Actor is who did something, and in which chat.
type Actor struct {
	UserID    int64
	ChatID    int64
	ChatKind  model.ChatKind
	ChatTitle string
}

// Waker is notified after any change that affects reminders or digests.
type Waker interface{ Wake() }

// Errors returned to handlers; handlers turn them into user-facing text.
var (
	ErrNotFound    = store.ErrNotFound
	ErrForeignChat = errors.New("item belongs to another chat")
	ErrBadTZ       = errors.New("unknown time zone")
	ErrBadLang     = errors.New("unsupported language")
	ErrBadCalendar = errors.New("unsupported calendar")
	ErrBadTime     = errors.New("invalid time, use HH:MM")
	ErrInPast      = errors.New("time is in the past")
)

// Service is the API used by handlers.
type Service interface {
	// Prefs returns the user's TZ and locale, creating the user on first
	// contact with defaults (config TZ, i18n.Default(tgLang)). tgLang may be "".
	Prefs(ctx context.Context, userID int64, tgLang string) (model.Prefs, error)
	// SetLang / SetCalendar change the user's locale.
	SetLang(ctx context.Context, userID int64, lang i18n.Lang) error
	SetCalendar(ctx context.Context, userID int64, cal calendar.Kind) error
	// AddTodo ensures user and chat exist and inserts the todo; a due time
	// creates a 'due' reminder.
	AddTodo(ctx context.Context, a Actor, r nlparse.Result) (model.Todo, error)
	// List returns open todos for page (0-based) and the total page count (>= 1).
	List(ctx context.Context, chatID int64, page int) ([]model.Todo, int, error)
	ByNum(ctx context.Context, chatID int64, num int) (model.Todo, error)
	Complete(ctx context.Context, a Actor, todoID int64) (model.Todo, error)
	Delete(ctx context.Context, a Actor, todoID int64) error
	Edit(ctx context.Context, a Actor, todoID int64, text string) (model.Todo, error)
	// Remind adds a 'custom' reminder; at must be in the future.
	Remind(ctx context.Context, a Actor, todoID int64, at time.Time) (model.Reminder, error)
	// Snooze creates a 'snooze' reminder; arg is ui.Snooze10m / Snooze1h / SnoozeTomorrow.
	Snooze(ctx context.Context, a Actor, reminderID int64, arg string) (model.Reminder, error)
	// AckReminder handles Done on a reminder message (see ARCHITECTURE §5).
	AckReminder(ctx context.Context, a Actor, reminderID int64) (model.Todo, error)
	// SetTZ stores the user's TZ, re-anchors their recurring todos and
	// recomputes digests they set.
	SetTZ(ctx context.Context, userID int64, tz string) error
	// SetDigest sets the chat's digest time in the actor's TZ and locale
	// (digest_by = actor); "" turns it off.
	SetDigest(ctx context.Context, a Actor, hhmm string) error
	DigestView(ctx context.Context, chatID int64, now time.Time) (model.DigestView, error)
	MigrateChat(ctx context.Context, oldID, newID int64) error
}

// Config holds service defaults.
type Config struct {
	DefaultTZ     *time.Location
	DefaultHour   int // TODO_DEFAULT_TIME, used for snooze "tomorrow"
	DefaultMinute int
}

// Svc implements Service and scheduler.Backend.
type Svc struct {
	st    store.Store
	clk   clock.Clock
	cfg   Config
	waker Waker
}

// New returns a Svc. Call SetWaker before the scheduler starts.
func New(st store.Store, clk clock.Clock, cfg Config) *Svc {
	return &Svc{st: st, clk: clk, cfg: cfg}
}

// SetWaker sets who is notified about schedule changes.
func (s *Svc) SetWaker(w Waker) { s.waker = w }

var _ Service = (*Svc)(nil)
