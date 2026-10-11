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

func button(text string, a Action) tg.Button { return tg.Button{Text: text, Data: Encode(a)} }

// dayNumber is the number of civil days since the epoch for t's date in loc.
func dayNumber(t time.Time, loc *time.Location) int {
	y, m, d := t.In(loc).Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// FormatWhen renders t in loc relative to now, in lc's language and calendar:
// en/gregorian "today 18:00", "tomorrow 09:00", "Tue 14 Oct 09:00", "14 Oct 2027 09:00";
// fa/jalali "امروز ۱۸:۰۰", "فردا ۰۹:۰۰", "سه‌شنبه ۲۲ مهر ۰۹:۰۰", "۲۲ مهر ۱۴۰۶ ۰۹:۰۰".
func FormatWhen(t time.Time, lc i18n.Locale, loc *time.Location, now time.Time) string {
	if loc == nil {
		loc = time.UTC
	}
	clock := lc.Digits(t.In(loc).Format("15:04"))
	diff := dayNumber(t, loc) - dayNumber(now, loc)
	switch diff {
	case 0:
		return lc.T("ui.when.today") + " " + clock
	case 1:
		return lc.T("ui.when.tomorrow") + " " + clock
	}
	cal := lc.Cal
	if !cal.Valid() {
		cal = calendar.Gregorian
	}
	d := cal.Split(t.In(loc))
	nd := cal.Split(now.In(loc))
	month := lc.MonthName(d.Month)
	if lc.Lang != i18n.Fa && cal == calendar.Gregorian {
		month = month[:3]
	}
	dm := lc.Num(d.Day) + " " + month
	if (diff >= -6 && diff <= 6) || d.Year == nd.Year {
		return lc.WeekdayName(t.In(loc).Weekday(), true) + " " + dm + " " + clock
	}
	return dm + " " + lc.Num(d.Year) + " " + clock
}

func isOverdue(td model.Todo, now time.Time) bool {
	return td.Status == model.TodoOpen && td.DueAt != nil && td.DueAt.Before(now)
}

func itemLine(td model.Todo, lc i18n.Locale, loc *time.Location, now time.Time) string {
	s := lc.Num(td.Num) + ". " + Escape(td.Text)
	if td.DueAt != nil {
		s += " — " + FormatWhen(*td.DueAt, lc, loc, now)
	}
	if isOverdue(td, now) {
		s += " ⚠️"
	}
	if td.Recur != "" {
		s += " 🔁" + ruleSuffix(td.Recur, lc)
	}
	return s + AssigneeSuffix(td, lc)
}

// AssigneeSuffix is " → @name" (a link by user ID) for an assigned item and
// "" for any other (#58).
func AssigneeSuffix(td model.Todo, lc i18n.Locale) string {
	if td.AssigneeID == 0 {
		return ""
	}
	return " → " + Mention(td.AssigneeID, td.AssigneeName, lc)
}

// Limits of the "Assigned to me" section of /list. A message holds 4096
// characters and an inline keyboard 100 buttons; a full page of own items
// uses 42 of them, so each assigned row has two buttons (Done, Delete).
const (
	MaxAssigned     = 20
	assignedTextMax = 80 // runes of an item's text shown in the section
	assignedBtnMax  = 20 // runes of an item's text shown on its Done button
)

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// TodoList renders one page of a list with per-item buttons and paging.
// page is 0-based; pages is the total page count (>= 1).
func TodoList(ts []model.Todo, lc i18n.Locale, loc *time.Location, now time.Time, page, pages int) tg.OutMessage {
	return TodoListAssigned(ts, nil, lc, loc, now, page, pages)
}

// TodoListAssigned is TodoList plus, on the first page, an "Assigned to me"
// section for items that live in other chats (#58). The section shows at
// most MaxAssigned items and then "and N more". Its items are reached only
// through their buttons: their numbers belong to other chats' lists.
func TodoListAssigned(ts, assigned []model.Todo, lc i18n.Locale, loc *time.Location, now time.Time, page, pages int) tg.OutMessage {
	if page != 0 {
		assigned = nil
	}
	if len(ts) == 0 && len(assigned) == 0 {
		return tg.OutMessage{Text: lc.T("ui.list.empty")}
	}
	var lines []string
	var kb [][]tg.Button
	if len(ts) > 0 {
		title := lc.T("ui.list.title")
		if pages > 1 {
			title = lc.T("ui.list.title.paged", lc.Num(page+1), lc.Num(pages))
		}
		lines = append(lines, title)
	}
	for _, td := range ts {
		lines = append(lines, itemLine(td, lc, loc, now))
		kb = append(kb, []tg.Button{
			button("✅ "+lc.Num(td.Num), Action{Kind: ActDone, ID: td.ID}),
			button("✏️", Action{Kind: ActEdit, ID: td.ID}),
			button("⏰", Action{Kind: ActRemind, ID: td.ID}),
			button("🗑", Action{Kind: ActDelete, ID: td.ID}),
		})
	}
	text := strings.Join(lines, "\n")
	if len(assigned) > 0 {
		shown := assigned[:min(len(assigned), MaxAssigned)]
		sec := []string{"<b>" + lc.T("ui.assign.title") + "</b>"}
		for _, td := range shown {
			line := "• " + Escape(clip(td.Text, assignedTextMax))
			if td.DueAt != nil {
				line += " — " + FormatWhen(*td.DueAt, lc, loc, now)
			}
			if isOverdue(td, now) {
				line += " ⚠️"
			}
			if td.Recur != "" {
				line += " 🔁" + ruleSuffix(td.Recur, lc)
			}
			sec = append(sec, line)
			kb = append(kb, []tg.Button{
				button("✅ "+clip(td.Text, assignedBtnMax), Action{Kind: ActDone, ID: td.ID}),
				button("🗑", Action{Kind: ActDelete, ID: td.ID}),
			})
		}
		if n := len(assigned) - len(shown); n > 0 {
			sec = append(sec, lc.T("ui.assign.more", lc.Num(n)))
		}
		if text != "" {
			text += "\n\n"
		}
		text += strings.Join(sec, "\n")
	}
	if pages > 1 {
		var row []tg.Button
		if page > 0 {
			row = append(row, button("‹", Action{Kind: ActPage, Arg: strconv.Itoa(page - 1)}))
		}
		if page < pages-1 {
			row = append(row, button("›", Action{Kind: ActPage, Arg: strconv.Itoa(page + 1)}))
		}
		kb = append(kb, row)
	}
	return tg.OutMessage{Text: text, Keyboard: kb}
}

// ReminderMsg renders a reminder with Done / 10m / 1h / Tomorrow buttons.
// late marks a reminder delivered more than 2 minutes after FireAt.
func ReminderMsg(dr model.DueReminder, lc i18n.Locale, loc *time.Location, now time.Time, late bool) tg.OutMessage {
	text := "⏰ " + Escape(dr.Todo.Text)
	if late {
		text += "\n" + lc.T("ui.reminder.late", FormatWhen(dr.FireAt, lc, loc, now))
	}
	snooze := func(key, arg string) tg.Button {
		return button(lc.T(key), Action{Kind: ActSnooze, ID: dr.ID, Arg: arg})
	}
	return tg.OutMessage{Text: text, Keyboard: [][]tg.Button{
		{button(lc.T("ui.btn.done"), Action{Kind: ActReminderDone, ID: dr.ID})},
		{
			snooze("ui.btn.snooze10m", Snooze10m),
			snooze("ui.btn.snooze1h", Snooze1h),
			snooze("ui.btn.tomorrow", SnoozeTomorrow),
		},
	}}
}

// Digest renders the daily digest (also used for /today) in v.Locale.
func Digest(v model.DigestView, now time.Time) tg.OutMessage {
	lc := v.Locale
	if len(v.Overdue) == 0 && len(v.Today) == 0 && v.NoDateCount == 0 {
		return tg.OutMessage{Text: lc.T("ui.digest.empty")}
	}
	var sections []string
	add := func(titleKey string, ts []model.Todo) {
		if len(ts) == 0 {
			return
		}
		lines := []string{"<b>" + lc.T(titleKey) + "</b>"}
		for _, td := range ts {
			lines = append(lines, itemLine(td, lc, v.Loc, now))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	add("ui.digest.overdue", v.Overdue)
	add("ui.digest.today", v.Today)
	if v.NoDateCount > 0 {
		sections = append(sections, lc.T("ui.digest.nodate", lc.Num(v.NoDateCount)))
	}
	return tg.OutMessage{Text: strings.Join(sections, "\n\n")}
}
