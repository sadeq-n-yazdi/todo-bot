// Package ui renders messages (HTML parse mode) and inline keyboards, and
// encodes callback data. It is pure: no I/O, no clock.
//
// All text goes through i18n (strings registered in this package's
// messages.go); dates are shown in the locale's calendar and digits.
package ui

import "errors"

// ActionKind identifies what an inline button does.
type ActionKind string

const (
	ActDone          ActionKind = "td" // complete todo ID
	ActDelete        ActionKind = "tx" // ask to delete todo ID
	ActDeleteConfirm ActionKind = "tX" // really delete todo ID
	ActEdit          ActionKind = "te" // prompt for new text of todo ID
	ActRemind        ActionKind = "tr" // prompt for reminder time of todo ID
	ActPage          ActionKind = "lp" // show list page Arg (ID unused)
	ActReminderDone  ActionKind = "rd" // Done pressed on reminder ID
	ActSnooze        ActionKind = "rs" // snooze reminder ID by Arg (see Snooze*)
	ActCancel        ActionKind = "cx" // dismiss a confirmation

	// Wizard buttons (#68, ARCHITECTURE §12). ID is the draft's Version
	// (> 0), so a press on an out-of-date card is detected; the draft is
	// found by the card the button is on. Arg values are listed by each
	// kind and never contain ":".
	ActWizStart  ActionKind = "ws" // ➕ under /list: start the wizard (ID 0, no Arg)
	ActWizDate   ActionKind = "wd" // Arg: WizDateNone, WizDateGrid, or a date "YYYYMMDD" (Gregorian)
	ActWizMonth  ActionKind = "wm" // show the month grid of Arg: "g202610" / "j140507" (calendar letter, year, month)
	ActWizTime   ActionKind = "wt" // Arg: "HHMM", WizTimeDefault or WizTimeType
	ActWizRepeat ActionKind = "wr" // Arg: one of WizRepeat*
	ActWizAssign ActionKind = "wa" // Arg: the assignee's user ID; "0" = me / nobody
	ActWizNav    ActionKind = "wn" // Arg: one of WizNav*
	ActWizEdit   ActionKind = "we" // Arg: the step to go back to: one of WizEdit*
	ActWizNoop   ActionKind = "wz" // not a choice (grid padding, headers, past days); Arg "" or WizNoopPast; ID may be 0
)

// Arguments of the wizard actions.
const (
	WizDateNone = "n" // ActWizDate: no date
	WizDateGrid = "o" // ActWizDate: open the month grid at the current month

	WizTimeDefault = "d" // ActWizTime: the default time (TODO_DEFAULT_TIME)
	WizTimeType    = "k" // ActWizTime: type a time (asks for a reply)

	WizRepeatOnce    = "o"
	WizRepeatDaily   = "d"
	WizRepeatWeekly  = "w"
	WizRepeatMonthly = "m"
	WizRepeatYearly  = "y"

	WizNavSkip     = "s" // ⏭ Skip
	WizNavBack     = "b" // ◀ Back
	WizNavCancel   = "x" // ✖ Cancel
	WizNavSave     = "v" // ✅ Save
	WizNavUseFound = "y" // "Found: tomorrow 09:00 — use it?" yes
	WizNavNoFound  = "n" // ... no

	WizEditText     = "text"
	WizEditDate     = "date"
	WizEditTime     = "time"
	WizEditRepeat   = "repeat"
	WizEditAssignee = "assignee"

	WizNoopPast = "p" // ActWizNoop on a past day: the handler says so
)

// Snooze arguments for ActSnooze.
const (
	Snooze10m      = "10m"
	Snooze1h       = "1h"
	SnoozeTomorrow = "tm"
)

// PageSize is the number of todos per /list page.
const PageSize = 10

// Action is decoded callback data.
type Action struct {
	Kind ActionKind
	ID   int64
	Arg  string
}

// ErrBadAction is returned by Decode for malformed data.
var ErrBadAction = errors.New("ui: bad callback data")
