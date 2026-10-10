// Package model holds plain domain types shared by every layer. It has no
// dependencies beyond the standard library.
package model

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// ChatKind distinguishes a personal list (private chat) from a shared one.
type ChatKind string

const (
	ChatPrivate ChatKind = "private"
	ChatGroup   ChatKind = "group"
)

// TodoStatus is the lifecycle state of a todo item.
type TodoStatus string

const (
	TodoOpen    TodoStatus = "open"
	TodoDone    TodoStatus = "done"
	TodoDeleted TodoStatus = "deleted"
)

// ReminderKind says why a reminder exists.
type ReminderKind string

const (
	// ReminderDue fires at the todo's due time; for recurring todos the
	// scheduler re-creates it for the next occurrence.
	ReminderDue ReminderKind = "due"
	// ReminderCustom is an extra reminder added with /remind.
	ReminderCustom ReminderKind = "custom"
	// ReminderSnooze is created by a snooze button.
	ReminderSnooze ReminderKind = "snooze"
)

// ReminderState is the delivery state of a reminder.
type ReminderState string

const (
	ReminderPending   ReminderState = "pending"
	ReminderSent      ReminderState = "sent"
	ReminderCancelled ReminderState = "cancelled"
	ReminderFailed    ReminderState = "failed"
)

// User is an allowlisted Telegram user who has talked to the bot.
type User struct {
	ID        int64
	TZ        string // IANA name, e.g. "Europe/Berlin"
	Lang      i18n.Lang
	Calendar  calendar.Kind
	CreatedAt time.Time
}

// Prefs is a user's resolved time zone and locale.
type Prefs struct {
	TZ     *time.Location
	Locale i18n.Locale
}

// Chat is a private chat (ID == user ID) or a group chat (shared list).
type Chat struct {
	ID           int64
	Kind         ChatKind
	Title        string
	DigestTime   string // "HH:MM" or "" when the digest is off
	DigestBy     int64  // user who set the digest; their TZ and locale are used
	DigestNextAt *time.Time
	CreatedAt    time.Time
}

// Todo is one item on a chat's list. Num is the short per-chat number shown
// to users; ID is the global primary key used in callback data.
type Todo struct {
	ID        int64
	ChatID    int64
	Num       int
	Text      string
	Status    TodoStatus
	DueAt     *time.Time
	Recur     string // canonical recur.Rule string; "" = not recurring
	TZ        string // creator's TZ; recurrence is computed in it
	CreatedBy int64
	CreatedAt time.Time
	UpdatedAt time.Time
	DoneAt    *time.Time
}

// Reminder is a scheduled notification for a todo.
type Reminder struct {
	ID        int64
	TodoID    int64
	ChatID    int64
	FireAt    time.Time
	Kind      ReminderKind
	State     ReminderState
	Attempts  int
	SentAt    *time.Time
	MessageID int // Telegram message ID once sent (for editing on Done)
	CreatedAt time.Time
}

// DueReminder is a pending reminder joined with its todo.
type DueReminder struct {
	Reminder
	Todo Todo
}

// TodoQuery filters ListTodos. Zero values mean "no filter"; Limit 0 = no limit.
type TodoQuery struct {
	ChatID    int64
	Statuses  []TodoStatus
	DueBefore *time.Time
	Limit     int
	Offset    int
}

// DigestView is what the daily digest shows for one chat.
type DigestView struct {
	ChatID      int64
	Loc         *time.Location
	Locale      i18n.Locale
	Overdue     []Todo
	Today       []Todo
	NoDateCount int
}
