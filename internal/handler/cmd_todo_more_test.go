package handler

import (
	"strings"
	"testing"
	"time"
)

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

func TestConfirmationRepliesStayShort(t *testing.T) {
	long := strings.Repeat("&", 1000)
	e := newTodoEnv(t)
	check := func(what string) {
		t.Helper()
		got := e.last(t).Msg.Text
		if n := utf16Len(got); n >= 4096 || !strings.Contains(got, "&amp;") {
			t.Fatalf("%s: reply has %d units: %.60q", what, n, got)
		}
	}
	e.say(userEn, userEn, "/add "+long)
	check("add")
	e.say(userEn, userEn, "/edit 1 "+long)
	check("edit")
	e.say(userEn, userEn, "/done 1")
	check("done")
	e.say(userEn, userEn, "/add "+long)
	e.say(userEn, userEn, "/del 2")
	check("del")
}

func TestAddJalaliFarsiDate(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userFa, userFa, "/add جلسه ۱۵ مهر ساعت ۹")
	td := e.svc.todos[0]
	if td.Text != "جلسه" || td.DueAt == nil {
		t.Fatalf("todo = %+v", td)
	}
	got := td.DueAt.UTC()
	t.Logf("due = %v", got)
	if got.Month() != time.October || got.Day() != 7 || got.Hour() != 9 {
		t.Fatalf("due = %v", got)
	}
}

func TestAddInNonUTCZone(t *testing.T) {
	e := newTodoEnv(t)
	// Now is 12:00 UTC = 15:30 in Tehran.
	e.say(userTeh, userTeh, "/add x tomorrow 18:00")
	want := time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC) // 18:00 +03:30
	if d := e.svc.todos[0].DueAt; d == nil || !d.Equal(want) {
		t.Fatalf("due = %v, want %v", d, want)
	}
	e.say(userTeh, userTeh, "/add y 14:00") // already past in Tehran: rolls to tomorrow
	want = time.Date(2026, 10, 11, 10, 30, 0, 0, time.UTC)
	if d := e.svc.todos[1].DueAt; d == nil || !d.Equal(want) {
		t.Fatalf("due = %v, want %v", d, want)
	}
}

func TestParseResultReachesService(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add x tomorrow")
	e.say(userEn, userEn, "/add x tomorrow 18:00")
	e.say(userEn, userEn, "/add x")
	a := e.svc.added
	if len(a) != 3 {
		t.Fatalf("got %d results", len(a))
	}
	if a[0].Due == nil || a[0].HasTime {
		t.Errorf("date only: %+v", a[0])
	}
	if a[1].Due == nil || !a[1].HasTime || a[1].Due.Hour() != 18 {
		t.Errorf("date and time: %+v", a[1])
	}
	if a[2].Due != nil || a[2].HasTime {
		t.Errorf("no date: %+v", a[2])
	}
}

func TestTodayFarsiWithItems(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userFa, userFa, "/add نان today 18:00")
	e.say(userFa, userFa, "/today")
	got := e.last(t).Msg.Text
	if !strings.Contains(got, "نان") || !strings.Contains(got, "۱۸") || !strings.Contains(got, "۱.") {
		t.Fatalf("fa today: %q", got)
	}
}

func TestEditSplitsOnAnyWhitespace(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add a")
	e.say(userEn, userEn, "/edit 1\n new  text")
	if got := e.svc.todos[0].Text; got != "new  text" {
		t.Fatalf("text = %q", got)
	}
	e.say(userEn, userEn, "/edit 1\tother")
	if got := e.svc.todos[0].Text; got != "other" {
		t.Fatalf("text = %q", got)
	}
}
