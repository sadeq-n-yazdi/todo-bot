// Package ui renders messages (HTML parse mode) and inline keyboards, and
// encodes callback data. It is pure: no I/O, no clock.
//
// All text goes through i18n (strings registered in this package's
// messages.go); dates are shown in the locale's calendar and digits.
package ui

import (
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

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

var errNotImplemented = errors.New("ui: not implemented")

// Encode returns callback data of at most 64 bytes.
func Encode(a Action) string { return "" } // TODO(#7)

// Decode parses data produced by Encode.
func Decode(s string) (Action, error) { return Action{}, errNotImplemented } // TODO(#7)

// TodoList renders one page of a list with per-item buttons and paging.
// page is 0-based; pages is the total page count (>= 1).
func TodoList(ts []model.Todo, lc i18n.Locale, loc *time.Location, now time.Time, page, pages int) tg.OutMessage {
	return tg.OutMessage{} // TODO(#7)
}

// ReminderMsg renders a reminder with Done / 10m / 1h / Tomorrow buttons.
// late marks a reminder delivered more than 2 minutes after FireAt.
func ReminderMsg(dr model.DueReminder, lc i18n.Locale, loc *time.Location, now time.Time, late bool) tg.OutMessage {
	return tg.OutMessage{} // TODO(#7)
}

// Digest renders the daily digest (also used for /today) in v.Locale.
func Digest(v model.DigestView, now time.Time) tg.OutMessage {
	return tg.OutMessage{} // TODO(#7)
}

// FormatWhen renders t in loc relative to now, in lc's language and calendar:
// en/gregorian "today 18:00", "tomorrow 09:00", "Tue 14 Oct 09:00", "14 Oct 2027 09:00";
// fa/jalali "امروز ۱۸:۰۰", "فردا ۰۹:۰۰", "سه‌شنبه ۲۲ مهر ۰۹:۰۰", "۲۲ مهر ۱۴۰۶ ۰۹:۰۰".
func FormatWhen(t time.Time, lc i18n.Locale, loc *time.Location, now time.Time) string {
	return "" // TODO(#7)
}

// Escape escapes s for HTML parse mode.
func Escape(s string) string { return "" } // TODO(#7)
