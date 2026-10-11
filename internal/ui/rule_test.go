package ui

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func TestDescribeRule(t *testing.T) {
	cases := []struct {
		rule, en, fa string
	}{
		{"daily 09:00", "every day", "هر روز"},
		{"weekly mon,wed 08:00", "every Mon, Wed", "هر دوشنبه، چهارشنبه"},
		{"weekly mon,tue,wed,thu,fri 09:30", "every weekday", "هر دوشنبه تا جمعه"},
		{"weekly sun,mon,tue,wed,thu,fri,sat 09:30", "every day", "هر روز"},
		{"weekly sat,sun 08:00", "every Sun, Sat", "هر شنبه، یکشنبه"},
		{"monthly 5 10:00", "every month on the 5th", "هر ماه روز ۵"},
		{"monthly 22 10:00", "every month on the 22nd", "هر ماه روز ۲۲"},
		{"monthly 11 10:00", "every month on the 11th", "هر ماه روز ۱۱"},
		{"monthly 1 10:00 jalali", "every Jalali month on the 1st", "هر ماه روز ۱"},
		{"monthly fri#last 16:00 jalali", "every month, last Friday", "هر ماه، آخرین جمعه"},
		{"monthly mon#2 09:00", "every month, 2nd Monday", "هر ماه، دومین دوشنبه"},
		{"monthly wed#4 09:00", "every month, 4th Wednesday", "هر ماه، چهارمین چهارشنبه"},
		{"yearly 01-27 09:00 jalali", "every year on 27 Farvardin", "هر سال ۲۷ فروردین"},
		{"yearly 04-16 09:00", "every year on 16 April", "هر سال ۱۶ آوریل"},
		{"every 3d 2026-10-10 09:00", "every 3 days", "هر ۳ روز"},
		{"every 1d 2026-10-10 09:00", "every day", "هر روز"},
		{"", "", ""},
		{"daily", "", ""},
		{"nonsense 1 2", "", ""},
		{"yearly 13-01 09:00", "", ""},
	}
	// The user's calendar must not matter: only the rule's own does.
	for _, cal := range []calendar.Kind{calendar.Gregorian, calendar.Jalali} {
		for _, c := range cases {
			if got := DescribeRule(c.rule, i18n.Locale{Lang: i18n.En, Cal: cal}); got != c.en {
				t.Errorf("en/%v %q = %q, want %q", cal, c.rule, got, c.en)
			}
			if got := DescribeRule(c.rule, i18n.Locale{Lang: i18n.Fa, Cal: cal}); got != c.fa {
				t.Errorf("fa/%v %q = %q, want %q", cal, c.rule, got, c.fa)
			}
		}
	}
}

func TestDescribeRuleCrossCalendar(t *testing.T) {
	// A Jalali rule for a Gregorian user, and the other way round.
	if got := DescribeRule("yearly 01-27 09:00 jalali", locEnG); got != "every year on 27 Farvardin" {
		t.Errorf("got %q", got)
	}
	if got := DescribeRule("yearly 04-16 09:00", locFaJ); got != "هر سال ۱۶ آوریل" {
		t.Errorf("got %q", got)
	}
}

func TestItemLineShowsRule(t *testing.T) {
	td := model.Todo{Num: 3, Text: "pay", Status: model.TodoOpen, DueAt: at(13, 18, 0), Recur: "yearly 01-27 09:00 jalali"}
	if got := itemLine(td, locEnG, testLoc, testNow); !strings.Contains(got, " 🔁 every year on 27 Farvardin") {
		t.Errorf("en: %q", got)
	}
	if got := itemLine(td, locFaJ, testLoc, testNow); !strings.Contains(got, " 🔁 هر سال ۲۷ فروردین") {
		t.Errorf("fa: %q", got)
	}
	td.Recur = "daily"
	if got := itemLine(td, locEnG, testLoc, testNow); !strings.HasSuffix(got, " 🔁") {
		t.Errorf("unparsable: %q", got)
	}
}

func TestRuleSuffixLongRowsFit(t *testing.T) {
	long := "weekly sun,mon,tue,wed,thu,fri 09:00"
	var own, assigned []model.Todo
	text := strings.Repeat("ک", 80)
	for i := 0; i < 10; i++ {
		own = append(own, model.Todo{ID: int64(i + 1), Num: 100 + i, Text: text, Status: model.TodoOpen, DueAt: at(20, 9, 0), Recur: long})
	}
	for i := 0; i < MaxAssigned; i++ {
		assigned = append(assigned, model.Todo{ID: int64(50 + i), Num: 200 + i, Text: text, Status: model.TodoOpen, DueAt: at(20, 9, 0), Recur: long})
	}
	m := TodoListAssigned(own, assigned, locFaJ, testLoc, testNow, 0, 2)
	if n := len(utf16.Encode([]rune(m.Text))); n > 4096 {
		t.Errorf("text has %d UTF-16 units", n)
	}
}
