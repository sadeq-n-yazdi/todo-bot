package ui

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

var (
	testLoc = time.FixedZone("T", 3*3600+1800)
	// Tuesday 13 Oct 2026 = 21 Mehr 1405.
	testNow = time.Date(2026, 10, 13, 10, 0, 0, 0, testLoc)

	locEnG = i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}
	locFaJ = i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}
	locEnJ = i18n.Locale{Lang: i18n.En, Cal: calendar.Jalali}
)

func at(d, h, m int) *time.Time {
	t := time.Date(2026, 10, d, h, m, 0, 0, testLoc)
	return &t
}

func sampleTodos() []model.Todo {
	return []model.Todo{
		{ID: 101, Num: 3, Text: "buy <milk> & eggs", Status: model.TodoOpen, DueAt: at(13, 18, 0), Recur: "daily 09:00"},
		{ID: 102, Num: 4, Text: "call mom", Status: model.TodoOpen, DueAt: at(12, 9, 0)},
		{ID: 103, Num: 12, Text: "no date", Status: model.TodoOpen},
	}
}

func sampleReminder() model.DueReminder {
	return model.DueReminder{
		Reminder: model.Reminder{ID: 45, FireAt: *at(13, 9, 0)},
		Todo:     model.Todo{ID: 101, Text: "pay <rent>"},
	}
}
