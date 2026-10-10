package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func TestCmdTZ(t *testing.T) {
	hh := newHarness12(t)

	hh.private("/tz")
	if !strings.Contains(hh.last().Msg.Text, "UTC") {
		t.Fatalf("should show the current zone: %q", hh.last().Msg.Text)
	}

	hh.private("/tz Europe/Berlin")
	if hh.svc.prefs[1].TZ.String() != "Europe/Berlin" || !strings.Contains(hh.last().Msg.Text, "Europe/Berlin") {
		t.Fatalf("tz not set: %v / %q", hh.svc.prefs[1].TZ, hh.last().Msg.Text)
	}

	for _, bad := range []string{"/tz Mars/Base", "/tz Local", "/tz ../etc/passwd"} {
		calls := hh.svc.called("SetTZ")
		hh.private(bad)
		if hh.svc.called("SetTZ") != calls {
			t.Errorf("%s reached the service", bad)
		}
		if !strings.Contains(hh.last().Msg.Text, "Berlin") {
			t.Errorf("%s: want a hint with an example, got %q", bad, hh.last().Msg.Text)
		}
	}
}

func TestCmdDigest(t *testing.T) {
	hh := newHarness12(t)

	hh.private("/digest 8:30")
	if hh.svc.digests[1] != "08:30" {
		t.Fatalf("digest = %q", hh.svc.digests[1])
	}
	hh.private("/digest ۲۱:۰۵") // Persian digits
	if hh.svc.digests[1] != "21:05" {
		t.Fatalf("digest = %q", hh.svc.digests[1])
	}
	hh.private("/digest off")
	if d, ok := hh.svc.digests[1]; !ok || d != "" {
		t.Fatalf("off should store empty, got %q ok=%v", d, ok)
	}
	n := hh.svc.called("SetDigest")
	hh.private("/digest 25:99")
	hh.private("/digest soon")
	hh.private("/digest")
	if hh.svc.called("SetDigest") != n {
		t.Fatal("invalid input must not reach the service")
	}
	if !strings.Contains(hh.last().Msg.Text, "/digest") {
		t.Fatalf("usage expected: %q", hh.last().Msg.Text)
	}
}

func TestCmdLang(t *testing.T) {
	hh := newHarness12(t)

	hh.private("/lang")
	kb := hh.last().Msg.Keyboard
	if len(kb) == 0 || len(kb[0]) != 2 {
		t.Fatalf("want two buttons, got %+v", hh.last().Msg)
	}
	for _, b := range kb[0] {
		if len(b.Data) > 64 {
			t.Errorf("callback data too long: %q", b.Data)
		}
	}

	hh.private("/lang fa")
	if hh.svc.prefs[1].Locale.Lang != i18n.Fa {
		t.Fatal("lang not set")
	}
	hh.private("/lang انگلیسی")
	if hh.svc.prefs[1].Locale.Lang != i18n.En {
		t.Fatal("Farsi alias for English not accepted")
	}
	hh.private("/lang فارسی")
	if hh.svc.prefs[1].Locale.Lang != i18n.Fa {
		t.Fatal("Farsi alias not accepted")
	}
	// Reply is now in Farsi, with Persian script.
	if !strings.ContainsAny(hh.last().Msg.Text, "فارسی") {
		t.Fatalf("expected a Farsi reply: %q", hh.last().Msg.Text)
	}
	n := hh.svc.called("SetLang")
	hh.private("/lang klingon")
	if hh.svc.called("SetLang") != n {
		t.Fatal("unknown language reached the service")
	}
}

func TestCmdCalendar(t *testing.T) {
	hh := newHarness12(t)

	hh.private("/calendar")
	if kb := hh.last().Msg.Keyboard; len(kb) == 0 || len(kb[0]) != 2 {
		t.Fatalf("want two buttons, got %+v", hh.last().Msg)
	}
	cases := map[string]calendar.Kind{
		"/calendar jalali":    calendar.Jalali,
		"/calendar Gregorian": calendar.Gregorian,
		"/calendar شمسی":      calendar.Jalali,
		"/calendar میلادی":    calendar.Gregorian,
	}
	for in, want := range cases {
		hh.private(in)
		if got := hh.svc.prefs[1].Locale.Cal; got != want {
			t.Errorf("%s: cal = %q, want %q", in, got, want)
		}
	}
	n := hh.svc.called("SetCalendar")
	hh.private("/calendar mayan")
	if hh.svc.called("SetCalendar") != n {
		t.Fatal("unknown calendar reached the service")
	}
}

func TestLangAndCalendarButtons(t *testing.T) {
	hh := newHarness12(t)
	hh.private("/lang")
	fa := hh.last().Msg.Keyboard[0][1]
	hh.press(1, 1, 31, fa.Data)
	if hh.svc.prefs[1].Locale.Lang != i18n.Fa {
		t.Fatalf("button did not set lang: %+v", hh.svc.prefs[1])
	}
	got := hh.last()
	if !got.Edit || got.MsgID != 31 || len(got.Msg.Keyboard) != 0 {
		t.Fatalf("want in-place confirmation: %+v", got)
	}

	hh.private("/calendar")
	jal := hh.last().Msg.Keyboard[0][1]
	hh.press(1, 1, 32, jal.Data)
	if hh.svc.prefs[1].Locale.Cal != calendar.Jalali {
		t.Fatalf("button did not set calendar: %+v", hh.svc.prefs[1])
	}

	hh.press(1, 1, 33, "sl:xx")
	if len(hh.svc.prefs) != 1 || hh.svc.called("SetLang") != 1 {
		t.Fatal("invalid setting button must not reach the service")
	}
}

func TestCmdRemindByNumber(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.addTodo(1, "call mom")

	hh.private("/remind 1 tomorrow 18:00")
	want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if len(hh.svc.remindAt) != 1 || !hh.svc.remindAt[0].Equal(want) {
		t.Fatalf("remindAt = %v", hh.svc.remindAt)
	}

	hh.private("/remind 1 yesterday 09:00")
	hh.private("/remind 9 tomorrow 18:00")
	if len(hh.svc.remindAt) != 1 {
		t.Fatal("past time / missing item must not create reminders")
	}
	if hh.last().Msg.Text == "" {
		t.Fatal("missing item should be explained")
	}
}

func TestCmdRemindCreatesItem(t *testing.T) {
	hh := newHarness12(t)
	hh.private("/remind pay rent tomorrow 18:00")
	if hh.svc.called("AddTodo") != 1 {
		t.Fatalf("AddTodo calls = %d", hh.svc.called("AddTodo"))
	}
	got := hh.svc.todo(1)
	if got.Text != "pay rent" || got.DueAt == nil || got.ChatID != 1 {
		t.Fatalf("todo = %+v", got)
	}

	// A leading number that is not an item reference stays part of the text.
	hh.private("/remind 2 pizzas tomorrow 18:00")
	if got := hh.svc.todo(2); got.Text != "2 pizzas" {
		t.Fatalf("todo = %+v", got)
	}

	n := hh.svc.called("AddTodo")
	hh.private("/remind")
	hh.private("/remind water plants")
	if hh.svc.called("AddTodo") != n {
		t.Fatal("no time, no item")
	}
	if hh.last().Msg.Text == "" {
		t.Fatal("usage expected")
	}
}

func TestSettingsRepliesAreLocalised(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.prefs[1] = model.Prefs{TZ: time.UTC, Locale: i18n.Default("fa")}
	hh.private("/tz")
	if hh.last().Msg.Text == "" || strings.Contains(hh.last().Msg.Text, "Your time zone") {
		t.Fatalf("expected Farsi text, got %q", hh.last().Msg.Text)
	}
}

func TestSettingsKeysHaveBothLanguages(t *testing.T) {
	en := map[string]bool{}
	for _, k := range i18n.Keys(i18n.En) {
		en[k] = true
	}
	for _, k := range i18n.Keys(i18n.Fa) {
		if strings.HasPrefix(k, "settings.") && !en[k] {
			t.Errorf("%s only in fa", k)
		}
		delete(en, k)
	}
	for k := range en {
		if strings.HasPrefix(k, "settings.") {
			t.Errorf("%s only in en", k)
		}
	}
}

func TestCmdRemindNumberHeuristic(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.addTodo(1, "first")
	hh.svc.addTodo(1, "second")
	hh.svc.addTodo(1, "third")

	// "pay rent tomorrow" is not a time phrase, so the 3 stays in the text.
	hh.private("/remind 3 pay rent tomorrow")
	if got := hh.svc.todo(4); got.Text != "3 pay rent" {
		t.Fatalf("todo = %+v", got)
	}
	if len(hh.svc.remindAt) != 0 {
		t.Fatal("no reminder on item 3 expected")
	}
	hh.private("/remind 2 pizzas tomorrow")
	if got := hh.svc.todo(5); got.Text != "2 pizzas" {
		t.Fatalf("todo = %+v", got)
	}
	hh.private("/remind 2 tomorrow 18:00")
	if len(hh.svc.remindAt) != 1 {
		t.Fatal("numeric form should add a reminder to item 2")
	}
}

func TestCmdRemindRejectsRecurringTextForm(t *testing.T) {
	hh := newHarness12(t)
	hh.private("/remind pay rent every month 10am")
	if hh.svc.called("AddTodo") != 0 {
		t.Fatal("recurring item must not be created")
	}
	if !strings.Contains(hh.last().Msg.Text, "/add") {
		t.Fatalf("want the recurring explanation: %q", hh.last().Msg.Text)
	}
}

func TestTZBadNameIsTruncated(t *testing.T) {
	hh := newHarness12(t)
	hh.private("/tz " + strings.Repeat("x", 3000))
	if n := len([]rune(hh.last().Msg.Text)); n > 400 {
		t.Fatalf("reply has %d runes", n)
	}
}
