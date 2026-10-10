package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func TestAssignedSection(t *testing.T) {
	lc := i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	var as []model.Todo
	for i := 1; i <= MaxAssigned+3; i++ {
		as = append(as, model.Todo{ID: int64(i), Text: "t<b>", Num: 99})
	}
	out := TodoListAssigned(nil, as, lc, time.UTC, now, 0, 1)
	if !strings.Contains(out.Text, "<b>Assigned to me</b>") || strings.HasPrefix(out.Text, lc.T("ui.list.empty")) {
		t.Fatalf("text = %q", out.Text)
	}
	if strings.Count(out.Text, "• t&lt;b&gt;") != MaxAssigned || !strings.Contains(out.Text, "…and 3 more") {
		t.Errorf("cap/more wrong: %q", out.Text)
	}
	if len(out.Keyboard) != MaxAssigned || out.Keyboard[0][0].Data != "td:1" || out.Keyboard[0][1].Data != "tx:1" {
		t.Errorf("keyboard = %+v", out.Keyboard)
	}
	if o := TodoListAssigned(nil, as, lc, time.UTC, now, 1, 2); strings.Contains(o.Text, "Assigned") {
		t.Error("section must show on the first page only")
	}
}

func TestRowShowsAssignee(t *testing.T) {
	lc := i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	row := itemLine(model.Todo{Num: 1, Text: "x", AssigneeID: 7, AssigneeName: "wife"}, lc, time.UTC, now)
	if !strings.HasSuffix(row, `→ <a href="tg://user?id=7">@wife</a>`) {
		t.Errorf("row = %q", row)
	}
	if got := itemLine(model.Todo{Num: 1, Text: "x"}, lc, time.UTC, now); strings.Contains(got, "→") {
		t.Errorf("unassigned row = %q", got)
	}
}
