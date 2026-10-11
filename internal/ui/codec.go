package ui

import (
	"strconv"
	"strings"
	"time"
)

// Encode returns callback data of at most 64 bytes: "kind:id" or
// "kind:id:arg". Only some kinds carry an arg (see argRule). An invalid
// arg or id is not repaired, which makes Decode reject the result.
func Encode(a Action) string {
	if !knownKind(a.Kind) {
		return ""
	}
	rule := argRule(a.Kind)
	if a.Kind == ActPage || a.Kind == ActWizStart {
		a.ID = 0
	}
	s := string(a.Kind) + ":" + strconv.FormatInt(a.ID, 10)
	if rule != argNone && a.Arg != "" && validArg(a.Kind, a.Arg) {
		s += ":" + a.Arg
	}
	return s
}

type argKind int

const (
	argNone     argKind = iota // never an arg; Encode drops one
	argRequired                // always an arg
	argOptional                // an arg or none
)

func argRule(k ActionKind) argKind {
	switch k {
	case ActSnooze, ActPage, ActWizDate, ActWizMonth, ActWizTime, ActWizRepeat, ActWizAssign, ActWizNav, ActWizEdit:
		return argRequired
	case ActWizNoop:
		return argOptional
	}
	return argNone
}

func validArg(k ActionKind, arg string) bool {
	switch k {
	case ActSnooze:
		return arg == Snooze10m || arg == Snooze1h || arg == SnoozeTomorrow
	case ActPage:
		return len(arg) <= 9 && isDigits(arg)
	case ActWizDate:
		return arg == WizDateNone || arg == WizDateGrid || arg == WizDateQuick || validDate(arg)
	case ActWizMonth:
		return validMonth(arg)
	case ActWizTime:
		return arg == WizTimeDefault || arg == WizTimeType || validHHMM(arg)
	case ActWizRepeat:
		return oneOf(arg, WizRepeatOnce, WizRepeatDaily, WizRepeatWeekly, WizRepeatMonthly, WizRepeatYearly)
	case ActWizAssign:
		if arg == "0" {
			return true
		}
		n, err := strconv.ParseInt(arg, 10, 64)
		return err == nil && n > 0 && strconv.FormatInt(n, 10) == arg
	case ActWizNav:
		return oneOf(arg, WizNavSkip, WizNavBack, WizNavCancel, WizNavSave, WizNavEdit, WizNavUseFound, WizNavNoFound)
	case ActWizEdit:
		return oneOf(arg, WizEditText, WizEditDate, WizEditTime, WizEditRepeat, WizEditAssignee)
	case ActWizNoop:
		return arg == "" || arg == WizNoopPast
	}
	return arg == ""
}

func oneOf(s string, set ...string) bool {
	for _, x := range set {
		if s == x {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validDate accepts a real Gregorian date "YYYYMMDD".
func validDate(s string) bool {
	if len(s) != 8 || !isDigits(s) {
		return false
	}
	t, err := time.Parse("20060102", s)
	return err == nil && t.Format("20060102") == s
}

// validMonth accepts "g" or "j" followed by "YYYYMM", month 01..12.
func validMonth(s string) bool {
	if len(s) != 7 || (s[0] != 'g' && s[0] != 'j') || !isDigits(s[1:]) {
		return false
	}
	y, _ := strconv.Atoi(s[1:5])
	m, _ := strconv.Atoi(s[5:])
	return y >= 1 && m >= 1 && m <= 12
}

// validHHMM accepts "HHMM", 0000..2359.
func validHHMM(s string) bool {
	if len(s) != 4 || !isDigits(s) {
		return false
	}
	h, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[2:])
	return h <= 23 && m <= 59
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
	if (k == ActPage || k == ActWizStart) && id != 0 {
		return Action{}, ErrBadAction
	}
	if isWizardChoice(k) && id <= 0 {
		return Action{}, ErrBadAction // a draft version is always > 0
	}
	if k == ActWizNoop && id < 0 {
		return Action{}, ErrBadAction
	}
	arg, hasArg := "", len(parts) == 3
	if hasArg {
		arg = parts[2]
	}
	switch argRule(k) {
	case argNone:
		if hasArg {
			return Action{}, ErrBadAction
		}
	case argRequired:
		if !hasArg || !validArg(k, arg) {
			return Action{}, ErrBadAction
		}
	case argOptional:
		if hasArg && (arg == "" || !validArg(k, arg)) {
			return Action{}, ErrBadAction
		}
	}
	return Action{Kind: k, ID: id, Arg: arg}, nil
}

// isWizardChoice reports whether k is a wizard button whose ID is a draft
// version.
func isWizardChoice(k ActionKind) bool {
	switch k {
	case ActWizDate, ActWizMonth, ActWizTime, ActWizRepeat, ActWizAssign, ActWizNav, ActWizEdit:
		return true
	}
	return false
}

// Escape escapes s for HTML parse mode.
func Escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func knownKind(k ActionKind) bool {
	switch k {
	case ActDone, ActDelete, ActDeleteConfirm, ActEdit, ActRemind, ActPage, ActReminderDone, ActSnooze, ActCancel,
		ActWizStart, ActWizDate, ActWizMonth, ActWizTime, ActWizRepeat, ActWizAssign, ActWizNav, ActWizEdit, ActWizNoop:
		return true
	}
	return false
}
