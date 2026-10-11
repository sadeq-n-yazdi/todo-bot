// Package service holds the business logic on top of store.Store. It also
// implements scheduler.Backend.
package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
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
	ErrEmptyText   = errors.New("empty text")
	// ErrStaleDraft: the wizard draft changed since it was read (another
	// button press, possibly on another node), or it was replaced, ended
	// or expired (#68).
	ErrStaleDraft = errors.New("wizard draft changed")
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
	// Todo returns the item with the global id, including done items. It
	// returns ErrNotFound if it is missing or deleted and ErrForeignChat
	// unless its ChatID equals a.ChatID or a.UserID is its assignee acting
	// from their own private chat (a.ChatID == a.UserID).
	Todo(ctx context.Context, a Actor, id int64) (model.Todo, error)
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

	// PutPrompt persists a force-reply prompt; ExpiresAt is set to now+PromptTTL.
	PutPrompt(ctx context.Context, p model.Prompt) error
	// TakePrompt consumes the prompt a reply (from userID) answers;
	// ErrNotFound if missing, another user's, expired or already taken (by
	// this or another node).
	TakePrompt(ctx context.Context, chatID int64, msgID int, userID int64) (model.Prompt, error)
	// MarkUpdate records a Telegram update id; first=false means it was
	// already handled (re-delivery after a poller failover or webhook retry).
	// Called only by handler.Handle, the single dedupe gate.
	MarkUpdate(ctx context.Context, updateID int64) (first bool, err error)

	// ---- Assignment (#58, ARCHITECTURE §11) ----
	//
	// Item actions (Todo, Complete, Delete, Edit, Remind, Snooze,
	// AckReminder) are allowed when the item belongs to a.ChatID, OR
	// a.UserID is its AssigneeID and the actor is in their own private
	// chat (a.ChatID == a.UserID); otherwise they fail with
	// ErrForeignChat. For Snooze and AckReminder the check is made on the
	// reminder's todo, not on the reminder's chat.

	// NoteUsername records username (Telegram @username without "@"; ""
	// = none) as userID's. It does nothing if the user does not exist yet
	// (the next update records the name), so it never fixes a first-time
	// user's language before Prefs applies it. It
	// writes only when the stored name differs (compared with the database
	// row, never an in-memory cache); a name taken over from another
	// account is cleared from that account. It calls store.SetUsername on
	// the pool, never inside WithTx, so concurrent nodes cannot make it
	// fail. The handler calls it for every update from an allowlisted
	// user, and only logs an error.
	NoteUsername(ctx context.Context, userID int64, username string) error
	// UserByUsername resolves a @username (case-insensitive, leading "@"
	// optional) to a user who has talked to the bot; ErrNotFound if none.
	// It does not check the allowlist: the handler does.
	UserByUsername(ctx context.Context, username string) (model.User, error)
	// AddTodoFor is AddTodo with an assignee. assigneeID 0, or the
	// assignee's own private chat (assigneeID == a.ChatID), stores no
	// assignee. ErrNotFound if the assignee does not exist. The returned
	// todo has AssigneeID and AssigneeName (the assignee's current
	// username) filled, as a store read would, so the handler can name the
	// assignee in its confirmation without another lookup. Every reminder
	// of the item (due, custom, snooze) is delivered to the assignee.
	AddTodoFor(ctx context.Context, a Actor, assigneeID int64, r nlparse.Result) (model.Todo, error)
	// ListAssigned returns the open items assigned to userID that live in
	// other chats than userID's private chat, ordered by due time (undated
	// last). /list in a private chat shows them under "Assigned to me".
	ListAssigned(ctx context.Context, userID int64) ([]model.Todo, error)

	// ---- Task-creation wizard (#68, ARCHITECTURE §12) ----
	//
	// Drafts live only in the database. Every change is a compare-and-set
	// on Draft.Version: a write based on an out-of-date read fails with
	// ErrStaleDraft and changes nothing. A draft expires DraftTTL (30m)
	// after its last write and then reads as ErrNotFound. Save is SaveDraft:
	// it ends the draft and creates the item in one transaction, through
	// the same code as AddTodoFor (/add).

	// StartDraft stores d as the draft of (d.ChatID, d.UserID), replacing
	// any earlier one. It sets d.UpdatedAt to now, d.ExpiresAt to
	// now+DraftTTL and d.Version.
	StartDraft(ctx context.Context, d *model.Draft) error
	// Draft returns the live draft of (chatID, userID); ErrNotFound if
	// none or expired.
	Draft(ctx context.Context, chatID, userID int64) (model.Draft, error)
	// DraftByMessage returns the live draft whose card is (chatID, msgID),
	// whoever started it; ErrNotFound if none or expired.
	DraftByMessage(ctx context.Context, chatID int64, msgID int) (model.Draft, error)
	// UpdateDraft writes d if d.Version is still the stored version,
	// extends its expiry to now+DraftTTL, and updates d.Version,
	// d.UpdatedAt and d.ExpiresAt. ErrStaleDraft if the draft was changed,
	// replaced, ended or expired since d was read.
	UpdateDraft(ctx context.Context, d *model.Draft) error
	// EndDraft deletes the draft (Cancel) if d.Version is still the stored
	// version and it has not expired; ErrStaleDraft otherwise. Exactly one
	// of several concurrent callers succeeds.
	EndDraft(ctx context.Context, d model.Draft) error
	// SaveDraft is Save. In one transaction it deletes the draft if
	// d.Version is still the stored version and it has not expired
	// (ErrStaleDraft otherwise, and no item is created), then does what
	// AddTodoFor(a, assigneeID, r) does. If that fails (ErrEmptyText, an
	// unknown assignee, ...) the transaction is rolled back and the draft
	// is still there with the same version. Of several concurrent calls
	// with one version, at most one creates an item.
	SaveDraft(ctx context.Context, d model.Draft, a Actor, assigneeID int64, r nlparse.Result) (model.Todo, error)
	// NamedUsers returns every user with a known @username except
	// exceptID, ordered by username. It does not check the allowlist: the
	// handler does.
	NamedUsers(ctx context.Context, exceptID int64) ([]model.User, error)
	// SetKBVersion records that the user's private chat now has version v
	// of the persistent reply keyboard (ui.NewTaskKeyboardVersion); Prefs
	// returns it as KBVersion. The user is created with defaults if new.
	// The handler calls it after a private reply carrying the keyboard was
	// sent.
	SetKBVersion(ctx context.Context, userID int64, v int) error
}

// Config holds service defaults.
type Config struct {
	DefaultTZ     *time.Location
	DefaultHour   int // TODO_DEFAULT_TIME, used for snooze "tomorrow"
	DefaultMinute int

	// NodeID identifies this process in claim tokens: each claim call uses a
	// fresh NodeID+":"+random token.
	NodeID string
	// ClaimTTL is how long a claimed reminder/digest stays reserved for this
	// node; 0 means 2m.
	ClaimTTL time.Duration
	// PromptTTL is how long a force-reply prompt stays answerable; 0 means 10m.
	PromptTTL time.Duration
	// DraftTTL is how long a wizard draft lives after its last change;
	// 0 means 30m.
	DraftTTL time.Duration
}

// Default claim, prompt and draft lifetimes used when Config leaves them zero.
const (
	defaultClaimTTL  = 2 * time.Minute
	defaultPromptTTL = 10 * time.Minute
	defaultDraftTTL  = 30 * time.Minute
)

// Svc implements Service and scheduler.Backend.
type Svc struct {
	st    store.Store
	clk   clock.Clock
	cfg   Config
	waker Waker
	log   *slog.Logger

	// retry is the transient-send-failure policy (backoff.go); tests replace it.
	retry func(attempts int) (time.Duration, bool)
}

// New returns a Svc. Call SetWaker before the scheduler starts.
func New(st store.Store, clk clock.Clock, cfg Config) *Svc {
	if cfg.DefaultTZ == nil {
		cfg.DefaultTZ = time.UTC
	}
	if cfg.ClaimTTL <= 0 {
		cfg.ClaimTTL = defaultClaimTTL
	}
	if cfg.PromptTTL <= 0 {
		cfg.PromptTTL = defaultPromptTTL
	}
	if cfg.DraftTTL <= 0 {
		cfg.DraftTTL = defaultDraftTTL
	}
	if cfg.NodeID == "" {
		cfg.NodeID = "node"
	}
	return &Svc{st: st, clk: clk, cfg: cfg, log: slog.Default(), retry: retryDelay}
}

// SetWaker sets who is notified about schedule changes.
func (s *Svc) SetWaker(w Waker) { s.waker = w }

// SetLogger sets the logger; the default is slog.Default().
func (s *Svc) SetLogger(l *slog.Logger) {
	if l != nil {
		s.log = l
	}
}

func (s *Svc) wake() {
	if s.waker != nil {
		s.waker.Wake()
	}
}

var (
	_ Service           = (*Svc)(nil)
	_ scheduler.Backend = (*Svc)(nil)
)
