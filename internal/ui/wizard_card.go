package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// wizardAsk is the implementation of WizardAsk.
func wizardAsk(step model.DraftStep, lc i18n.Locale) tg.OutMessage {
	key := "ui.wizard.q.text"
	if step == model.StepTimeText {
		key = "ui.wizard.q.timetext"
	}
	return tg.OutMessage{Text: lc.T(key), ForceReply: true}
}

// wizardCard is the implementation of WizardCard.
func wizardCard(v WizardView, lc i18n.Locale, loc *time.Location, now time.Time) tg.OutMessage {
	if loc == nil {
		loc = time.UTC
	}
	cal := lc.Cal
	if !cal.Valid() {
		cal = calendar.Gregorian
	}
	ver := v.Draft.Version
	nav := func(label, arg string) tg.Button {
		return button(lc.T(label), Action{Kind: ActWizNav, ID: ver, Arg: arg})
	}
	skip := func() tg.Button { return nav("ui.wizard.btn.skip", WizNavSkip) }
	back := func() tg.Button { return nav("ui.wizard.btn.back", WizNavBack) }
	cancel := func() tg.Button { return nav("ui.wizard.btn.cancel", WizNavCancel) }

	var text string
	var kb [][]tg.Button
	switch v.Draft.Step {
	case model.StepText:
		text = lc.T("ui.wizard.q.text")
		kb = [][]tg.Button{{cancel()}}
	case model.StepFound:
		text = lc.T("ui.wizard.q.found", foundWhen(v.Draft.Data, lc, loc, now))
		kb = [][]tg.Button{
			{nav("ui.wizard.btn.yes", WizNavUseFound), nav("ui.wizard.btn.no", WizNavNoFound)},
			{back(), cancel()},
		}
	case model.StepDate:
		if v.Grid != nil {
			y, m, d := now.In(loc).Date()
			text, kb = gridKeyboard(v, lc, cal, calendar.Date{Year: y, Month: int(m), Day: d})
		} else {
			text = lc.T("ui.wizard.q.date")
			kb = dateQuickKeyboard(v, lc, cal, loc, now)
		}
	case model.StepTime:
		text = lc.T("ui.wizard.q.time")
		kb = timeKeyboard(v, lc)
	case model.StepTimeText:
		text = lc.T("ui.wizard.q.timetext")
		kb = [][]tg.Button{{back(), cancel()}}
	case model.StepRepeat:
		text = lc.T("ui.wizard.q.repeat")
		kb = repeatKeyboard(v, lc, cal)
	case model.StepAssignee:
		text = lc.T("ui.wizard.q.assignee")
		kb = assigneeKeyboard(v, lc)
	case model.StepConfirm:
		if v.EditMenu {
			text = lc.T("ui.wizard.q.edit")
			kb = editKeyboard(v, lc)
		} else {
			head, saveKey := lc.T("ui.wizard.q.confirm"), "ui.wizard.btn.save"
			if v.EditNum != 0 {
				head, saveKey = lc.T("ui.wizard.edit.head", lc.Num(v.EditNum)), "ui.wizard.edit.save"
			}
			text = head + "\n\n" + summary(v, lc, cal, loc, now)
			kb = [][]tg.Button{{
				nav(saveKey, WizNavSave),
				nav("ui.wizard.btn.edit", WizNavEdit),
				cancel(),
			}}
		}
	default:
		return tg.OutMessage{}
	}
	// Steps that may be skipped or stepped back from end with ⏭ ◀ ✖.
	switch v.Draft.Step {
	case model.StepDate:
		if v.Grid == nil {
			kb = append(kb, []tg.Button{skip(), back(), cancel()})
		}
	case model.StepTime, model.StepRepeat, model.StepAssignee:
		kb = append(kb, []tg.Button{skip(), back(), cancel()})
	}
	if v.Note != "" {
		text = Escape(clip(v.Note, wizardNoteMax)) + "\n\n" + text
	}
	return tg.OutMessage{Text: text, Keyboard: kb}
}

func foundWhen(d model.DraftData, lc i18n.Locale, loc *time.Location, now time.Time) string {
	if d.FoundDue == nil {
		return ""
	}
	s := FormatWhen(*d.FoundDue, lc, loc, now)
	if !d.FoundTime {
		if i := strings.LastIndex(s, " "); i > 0 {
			s = s[:i] // FormatWhen always ends with the clock
		}
	}
	if d.FoundRule != "" {
		s += " 🔁"
	}
	return s
}

// dateQuickKeyboard: Today, Tomorrow, the next 5 weekdays by name, Other
// date, No date.
func dateQuickKeyboard(v WizardView, lc i18n.Locale, cal calendar.Kind, loc *time.Location, now time.Time) [][]tg.Button {
	ver := v.Draft.Version
	y, m, d := now.In(loc).Date()
	pick := func(label string, k int) tg.Button {
		t := time.Date(y, m, d+k, 12, 0, 0, 0, time.UTC) // calendar days, never 24h steps
		ty, tm, td := t.Date()
		return button(label, Action{Kind: ActWizDate, ID: ver, Arg: DateArg(calendar.Date{Year: ty, Month: int(tm), Day: td})})
	}
	kb := [][]tg.Button{{pick(lc.T("ui.wizard.btn.today"), 0), pick(lc.T("ui.wizard.btn.tmrw"), 1)}}
	var row []tg.Button
	for k := 2; k <= 6; k++ {
		t := time.Date(y, m, d+k, 12, 0, 0, 0, time.UTC)
		label := lc.WeekdayName(t.Weekday(), true) + " " + lc.Num(cal.Split(t).Day)
		row = append(row, pick(label, k))
		if len(row) == 3 || k == 6 {
			kb = append(kb, row)
			row = nil
		}
	}
	return append(kb, []tg.Button{
		button(lc.T("ui.wizard.btn.other"), Action{Kind: ActWizDate, ID: ver, Arg: WizDateGrid}),
		button(lc.T("ui.wizard.btn.nodate"), Action{Kind: ActWizDate, ID: ver, Arg: WizDateNone}),
	})
}

func timeKeyboard(v WizardView, lc i18n.Locale) [][]tg.Button {
	ver := v.Draft.Version
	var kb [][]tg.Button
	var row []tg.Button
	for i, s := range WizardTimes {
		hh, _ := strconv.Atoi(s[:2])
		mm, _ := strconv.Atoi(s[3:])
		row = append(row, button(lc.Digits(s), Action{Kind: ActWizTime, ID: ver, Arg: TimeArg(hh, mm)}))
		if len(row) == 3 || i == len(WizardTimes)-1 {
			kb = append(kb, row)
			row = nil
		}
	}
	return append(kb, []tg.Button{
		button(lc.T("ui.wizard.btn.type"), Action{Kind: ActWizTime, ID: ver, Arg: WizTimeType}),
		button(lc.T("ui.wizard.btn.default", lc.Digits(v.DefaultTime)), Action{Kind: ActWizTime, ID: ver, Arg: WizTimeDefault}),
	})
}

// dateOf parses Data.Date ("YYYY-MM-DD") as a UTC midnight.
func dateOf(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

func repeatKeyboard(v WizardView, lc i18n.Locale, cal calendar.Kind) [][]tg.Button {
	ver := v.Draft.Version
	weekly, monthly := lc.T("ui.wizard.repeat.w"), lc.T("ui.wizard.repeat.m")
	if t, ok := dateOf(v.Draft.Data.Date); ok {
		weekly = lc.T("ui.wizard.repeat.w.on", lc.WeekdayName(t.Weekday(), false))
		monthly = lc.T("ui.wizard.repeat.m.on", lc.Num(cal.Split(t).Day))
	}
	b := func(label, arg string) tg.Button { return button(label, Action{Kind: ActWizRepeat, ID: ver, Arg: arg}) }
	kb := [][]tg.Button{
		{b(lc.T("ui.wizard.repeat.once"), WizRepeatOnce), b(lc.T("ui.wizard.repeat.d"), WizRepeatDaily)},
		{b(weekly, WizRepeatWeekly), b(monthly, WizRepeatMonthly)},
		{b(lc.T("ui.wizard.repeat.y"), WizRepeatYearly)},
	}
	if t, ok := dateOf(v.Draft.Data.Date); ok {
		nth, hasNth, hasLast := NthWeekdayChoices(cal, t)
		var row []tg.Button
		if hasNth {
			row = append(row, b(nthLabel(lc, nth, t.Weekday()), WizRepeatNth))
		}
		if hasLast {
			row = append(row, b(lastLabel(lc, t.Weekday()), WizRepeatLast))
		}
		if len(row) > 0 {
			kb = append(kb, row)
		}
	}
	return kb
}

// NthWeekdayChoices says which "monthly on a weekday" choices the date t
// (a civil date) allows in calendar cal: the Nth weekday of the month,
// where nth is 1..4 and hasNth is false for the 5th, and the last such
// weekday of the month. A date can allow both (for example day 28 of a
// 28-day month) or one of them. Every date allows at least one.
func NthWeekdayChoices(cal calendar.Kind, t time.Time) (nth int, hasNth, hasLast bool) {
	cd := cal.Split(t)
	nth = (cd.Day-1)/7 + 1
	return nth, nth <= 4, cd.Day+7 > cal.MonthLength(cd.Year, cd.Month)
}

func nthLabel(lc i18n.Locale, nth int, w time.Weekday) string {
	return lc.T("ui.wizard.repeat.nth."+strconv.Itoa(nth), lc.WeekdayName(w, false))
}

func lastLabel(lc i18n.Locale, w time.Weekday) string {
	return lc.T("ui.wizard.repeat.last", lc.WeekdayName(w, false))
}

// Limits that keep a card under Telegram's 4096 characters and 100 buttons
// (with the worst case of escaping, 5x for "&", included).
const (
	maxWizardMembers = 85 // plus Nobody, Me, Skip, Back, Cancel: at most 90 buttons
	wizardTextMax    = 300
	wizardNoteMax    = 200
)

func memberLabel(u model.User, lc i18n.Locale) string {
	if u.Username == "" {
		return lc.T("ui.mention.noname")
	}
	return "@" + u.Username
}

func assigneeKeyboard(v WizardView, lc i18n.Locale) [][]tg.Button {
	ver := v.Draft.Version
	b := func(label, arg string) tg.Button { return button(label, Action{Kind: ActWizAssign, ID: ver, Arg: arg}) }
	me := b(lc.T("ui.wizard.btn.me"), WizAssignMe)
	var kb [][]tg.Button
	if v.Group {
		kb = append(kb, []tg.Button{b(lc.T("ui.wizard.btn.nobody"), WizAssignNone), me})
	} else {
		kb = append(kb, []tg.Button{me})
	}
	for _, u := range v.Members[:min(len(v.Members), maxWizardMembers)] {
		kb = append(kb, []tg.Button{b(memberLabel(u, lc), strconv.FormatInt(u.ID, 10))})
	}
	return kb
}

func editKeyboard(v WizardView, lc i18n.Locale) [][]tg.Button {
	ver := v.Draft.Version
	b := func(key, arg string) tg.Button {
		return button(lc.T(key), Action{Kind: ActWizEdit, ID: ver, Arg: arg})
	}
	return [][]tg.Button{
		{b("ui.wizard.edit.text", WizEditText), b("ui.wizard.edit.date", WizEditDate)},
		{b("ui.wizard.edit.time", WizEditTime), b("ui.wizard.edit.repeat", WizEditRepeat)},
		{b("ui.wizard.edit.who", WizEditAssignee)},
		{button(lc.T("ui.wizard.btn.back"), Action{Kind: ActWizNav, ID: ver, Arg: WizNavBack})},
	}
}

// summary is the confirm step's text: the item, when, repeat and assignee.
func summary(v WizardView, lc i18n.Locale, cal calendar.Kind, loc *time.Location, now time.Time) string {
	d := v.Draft.Data
	lines := []string{lc.T("ui.wizard.sum.text", Escape(clip(d.Text, wizardTextMax)))}
	switch t, ok := dateOf(d.Date); {
	case ok:
		hhmm := d.Time
		if hhmm == "" {
			hhmm = v.DefaultTime
		}
		hh, mm := 9, 0
		if p, err := time.Parse("15:04", hhmm); err == nil {
			hh, mm = p.Hour(), p.Minute()
		}
		at := time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, loc)
		lines = append(lines, lc.T("ui.wizard.sum.when", FormatWhen(at, lc, loc, now)))
	case d.FoundDue != nil:
		lines = append(lines, lc.T("ui.wizard.sum.when", foundWhen(model.DraftData{FoundDue: d.FoundDue, FoundTime: d.FoundTime}, lc, loc, now)))
	default:
		lines = append(lines, lc.T("ui.wizard.sum.nodate"))
	}
	if r := repeatLabel(v, lc, cal); r != "" {
		lines = append(lines, lc.T("ui.wizard.sum.repeat", r))
	}
	lines = append(lines, lc.T("ui.wizard.sum.who", assigneeLabel(v, lc)))
	return strings.Join(lines, "\n")
}

func repeatLabel(v WizardView, lc i18n.Locale, cal calendar.Kind) string {
	d := v.Draft.Data
	if d.Date == "" {
		if d.FoundDue != nil && d.FoundRule != "" {
			if v.EditNum != 0 {
				if s := DescribeRule(d.FoundRule, lc); s != "" {
					return Escape(s)
				}
			}
			return lc.T("ui.wizard.repeat.text")
		}
		return ""
	}
	switch d.Repeat {
	case model.RepeatDaily:
		return lc.T("ui.wizard.repeat.d")
	case model.RepeatWeekly:
		return lc.T("ui.wizard.repeat.w")
	case model.RepeatMonthly:
		return lc.T("ui.wizard.repeat.m")
	case model.RepeatYearly:
		return lc.T("ui.wizard.repeat.y")
	case model.RepeatNthWeekday, model.RepeatLastWeekday:
		t, ok := dateOf(d.Date)
		if !ok {
			return lc.T("ui.wizard.repeat.m")
		}
		if d.Repeat == model.RepeatLastWeekday {
			return lastLabel(lc, t.Weekday())
		}
		nth, _, _ := NthWeekdayChoices(cal, t)
		return nthLabel(lc, min(max(nth, 1), 4), t.Weekday())
	}
	return lc.T("ui.wizard.repeat.once")
}

func assigneeLabel(v WizardView, lc i18n.Locale) string {
	id := v.Draft.Data.AssigneeID
	switch {
	case id == 0 && v.Group:
		return lc.T("ui.wizard.who.nobody")
	case id == 0 || id == v.Draft.UserID:
		return lc.T("ui.wizard.who.me")
	}
	return Mention(id, v.AssigneeName, lc)
}
