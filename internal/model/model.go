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
	// Username is the user's Telegram @username, lowercase and without "@";
	// "" if unknown. It is written only by store.SetUsername (UpsertUser
	// ignores it) and is unique among users: a name seen on a new account is
	// cleared from the old one.
	Username string
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
	// DigestClaimToken is set on chats returned by ClaimDigests; conditional
	// writes (AdvanceDigest) succeed only while this claim is still ours.
	DigestClaimToken string
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
	// AssigneeID is the user the item is assigned to (/add @name ...); 0 =
	// none. Reminders of an assigned item go to the assignee's private chat
	// (chat ID == AssigneeID); the item stays on ChatID's list.
	AssigneeID int64
	// AssigneeName is the assignee's current Username, filled by the store
	// on every read; "" if there is no assignee or no known username.
	// Read-only: InsertTodo and UpdateTodo ignore it.
	AssigneeName string
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
	// ClaimToken is set on reminders returned by ClaimReminders; conditional
	// writes (RescheduleClaimed) succeed only while this claim is still ours.
	ClaimToken string
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

// Lease is a named, time-limited exclusive role shared through the database
// (e.g. "poller": the one node that calls getUpdates). Epoch increments every
// time the holder changes, so a node can tell a fresh win from a renewal.
type Lease struct {
	Name   string
	Holder string
	Until  time.Time
	Epoch  int64
}

// PromptKind says what a pending force-reply prompt is waiting for.
type PromptKind string

const (
	PromptEdit   PromptKind = "edit"   // new text for TodoID
	PromptRemind PromptKind = "remind" // a time for TodoID's reminder
)

// Prompt is a force-reply question the bot sent (MsgID in ChatID) to UserID;
// the user's reply to that message completes it. Persisted so any node, and
// a restarted one, can complete it.
type Prompt struct {
	ChatID    int64
	MsgID     int
	UserID    int64
	Kind      PromptKind
	TodoID    int64
	ExpiresAt time.Time
}

// DraftStep is where a task-creation wizard is (#68, ARCHITECTURE §12).
type DraftStep string

const (
	StepText     DraftStep = "text"      // waiting for the item text (a reply)
	StepFound    DraftStep = "found"     // the text had a date: use it?
	StepDate     DraftStep = "date"      // quick picks, month grid, no date
	StepTime     DraftStep = "time"      // quick picks, type a time, default
	StepTimeText DraftStep = "time_text" // waiting for a typed time (a reply)
	StepRepeat   DraftStep = "repeat"    // once, daily, weekly, monthly, yearly
	StepAssignee DraftStep = "assignee"  // me / nobody, or a known member
	StepConfirm  DraftStep = "confirm"   // summary: save, edit, cancel
)

// Repeat choices of the wizard's repeat step (DraftData.Repeat).
const (
	RepeatOnce    = ""
	RepeatDaily   = "daily"
	RepeatWeekly  = "weekly"  // on the chosen date's weekday
	RepeatMonthly = "monthly" // on the chosen day of the month, in the user's calendar
	RepeatYearly  = "yearly"  // on the chosen month and day, in the user's calendar
)

// DraftData is what a wizard has collected so far. The store keeps it as a
// JSON object, so a field added here needs no migration: an older row
// decodes with the new field at its zero value. Every field is optional
// and its zero value means "not chosen".
type DraftData struct {
	// Text is the item text, after i18n.NormalizeDigits. When a found date
	// was accepted, it is FoundText (the date phrase removed).
	Text string `json:"text,omitempty"`
	// Date is the chosen civil date, Gregorian "YYYY-MM-DD", in the user's
	// time zone. "" = no date.
	Date string `json:"date,omitempty"`
	// Time is the chosen time of day "HH:MM". "" = the default time
	// (TODO_DEFAULT_TIME).
	Time string `json:"time,omitempty"`
	// Repeat is one of the Repeat* constants.
	Repeat string `json:"repeat,omitempty"`
	// AssigneeID is the member the item is for; 0 = the actor / nobody.
	AssigneeID int64 `json:"assignee,omitempty"`
	// Found* hold what nlparse recognised in the text (StepFound): the
	// text without the date phrase, the first due time (UTC), the
	// canonical recur string ("" = once) and whether a time of day was
	// named. FoundDue nil = nothing found.
	FoundText string     `json:"found_text,omitempty"`
	FoundDue  *time.Time `json:"found_due,omitempty"`
	FoundRule string     `json:"found_rule,omitempty"`
	FoundTime bool       `json:"found_time,omitempty"`
	// Editing is set by Edit on the confirm step: the next answer returns
	// to StepConfirm instead of going on to the following step.
	Editing bool `json:"editing,omitempty"`
}

// Draft is one in-progress wizard, one per (ChatID, UserID). It lives only
// in the database, so any node, or a restarted one, can continue it.
type Draft struct {
	ChatID int64
	UserID int64 // who started it; only they may press its buttons
	// MsgID is the wizard card: the message with the inline keyboard that
	// is edited at every step. Button presses find the draft by it.
	MsgID int
	// AskMsgID is a separate ForceReply question for a text step (groups),
	// 0 if none. A text answer may reply to MsgID or to AskMsgID.
	AskMsgID int
	Step     DraftStep
	Data     DraftData
	// Version is the compare-and-set token. The store sets it on every
	// write; UpdateDraft and DeleteDraft succeed only while it matches.
	Version   int64
	UpdatedAt time.Time
	ExpiresAt time.Time // UpdatedAt + DraftTTL: 30 minutes of inactivity
}
