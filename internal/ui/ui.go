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
