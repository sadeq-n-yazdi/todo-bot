package ui

import (
	"strconv"
	"strings"
)

// Encode returns callback data of at most 64 bytes: "kind:id" or
// "kind:id:arg" (arg only for ActSnooze and ActPage). An invalid arg is
// dropped, which makes Decode reject the result.
func Encode(a Action) string {
	if !knownKind(a.Kind) {
		return ""
	}
	if a.Kind == ActPage {
		a.ID = 0
	}
	s := string(a.Kind) + ":" + strconv.FormatInt(a.ID, 10)
	if (a.Kind == ActSnooze || a.Kind == ActPage) && validArg(a.Kind, a.Arg) {
		s += ":" + a.Arg
	}
	return s
}

func validArg(k ActionKind, arg string) bool {
	switch k {
	case ActSnooze:
		return arg == Snooze10m || arg == Snooze1h || arg == SnoozeTomorrow
	case ActPage:
		if arg == "" || len(arg) > 9 {
			return false
		}
		for _, r := range arg {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	return arg == ""
}

// Decode parses data produced by Encode.
func Decode(s string) (Action, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Action{}, ErrBadAction
	}
	k := ActionKind(parts[0])
	if !knownKind(k) {
		return Action{}, ErrBadAction
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != parts[1] {
		return Action{}, ErrBadAction
	}
	if k == ActPage && id != 0 {
		return Action{}, ErrBadAction
	}
	arg := ""
	if len(parts) == 3 {
		arg = parts[2]
	}
	needsArg := k == ActSnooze || k == ActPage
	if needsArg != (len(parts) == 3) || !validArg(k, arg) {
		return Action{}, ErrBadAction
	}
	return Action{Kind: k, ID: id, Arg: arg}, nil
}

// Escape escapes s for HTML parse mode.
func Escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func knownKind(k ActionKind) bool {
	switch k {
	case ActDone, ActDelete, ActDeleteConfirm, ActEdit, ActRemind, ActPage, ActReminderDone, ActSnooze, ActCancel:
		return true
	}
	return false
}
