package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
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
	hh.press(1, 1, 34, "sl:0:xx")
	hh.press(1, 1, 35, "sc:0:mayan")
	if len(hh.svc.prefs) != 1 || hh.svc.called("SetLang") != 1 || hh.svc.called("SetCalendar") != 1 {
		t.Fatal("invalid setting button must not reach the service")
	}
}

// The buttons carry ui.Encode data.
func TestSettingsButtonsUseUICodec(t *testing.T) {
	hh := newHarness12(t)
	hh.private("/lang")
	row := hh.last().Msg.Keyboard[0]
	want := []string{ui.Encode(ui.Action{Kind: ui.ActSetLang, Arg: "en"}), ui.Encode(ui.Action{Kind: ui.ActSetLang, Arg: "fa"})}
	if len(row) != 2 || row[0].Data != want[0] || row[1].Data != want[1] {
		t.Fatalf("lang buttons %+v, want %v", row, want)
	}
	hh.private("/calendar")
	row = hh.last().Msg.Keyboard[0]
	want = []string{ui.Encode(ui.Action{Kind: ui.ActSetCal, Arg: "gregorian"}), ui.Encode(ui.Action{Kind: ui.ActSetCal, Arg: "jalali"})}
	if len(row) != 2 || row[0].Data != want[0] || row[1].Data != want[1] {
		t.Fatalf("calendar buttons %+v, want %v", row, want)
	}
	for _, b := range row {
		if _, err := ui.Decode(b.Data); err != nil || len(b.Data) > 64 {
			t.Fatalf("button data %q: %v", b.Data, err)
		}
	}
}

// Legacy "sl:<lang>" / "sc:<calendar>" data from messages sent before the
// ui codec is still accepted. Remove with legacySetting in v0.1.5.
func TestLegacySettingButtons(t *testing.T) {
	hh := newHarness12(t)
	hh.press(1, 1, 41, "sl:fa")
	if hh.svc.prefs[1].Locale.Lang != i18n.Fa {
		t.Fatalf("legacy lang button ignored: %+v", hh.svc.prefs[1])
	}
	hh.press(1, 1, 42, "sc:jalali")
	if hh.svc.prefs[1].Locale.Cal != calendar.Jalali {
		t.Fatalf("legacy calendar button ignored: %+v", hh.svc.prefs[1])
	}
	for _, d := range []string{"sl:", "sc:", "sx:fa", "sl:0:fa", "td:1"} {
		if a, ok := legacySetting(d); ok {
			t.Errorf("legacySetting(%q) = %+v, want no match", d, a)
		}
	}
}

// A private chat's language button resets the reply-keyboard version.
func TestLangButtonResetsKBVersion(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/tz", 0)
	e.press(wzAlice, wzAlice, 5, ui.Encode(ui.Action{Kind: ui.ActSetLang, Arg: "fa"}))
	if p, _ := e.svc.Prefs(context.Background(), wzAlice, ""); p.Locale.Lang != i18n.Fa || p.KBVersion != 0 {
		t.Fatalf("prefs %+v after the private language button", p)
	}
	e.msg(wzAlice, wzAlice, "/tz", 0)
	e.press(wzAlice, wzGrp, 6, ui.Encode(ui.Action{Kind: ui.ActSetLang, Arg: "en"}))
	if p, _ := e.svc.Prefs(context.Background(), wzAlice, ""); p.KBVersion != ui.NewTaskKeyboardVersion {
		t.Fatalf("KBVersion %d after a group button", p.KBVersion)
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

	hh.private("/remind 1 today 9am")
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

func TestCmdRemindServiceEmptyText(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.emptyText = "ghost"
	hh.private("/remind ghost tomorrow 18:00")
	if !strings.Contains(hh.last().Msg.Text, "empty") {
		t.Fatalf("want a localised explanation: %+v", hh.last())
	}
	if len(hh.svc.todos) != 0 {
		t.Fatal("no item expected")
	}
}

func TestCmdRemindPastTimeBothForms(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.addTodo(1, "call mom")
	for _, in := range []string{"/remind 1 today 9am", "/remind pay rent today 9am"} {
		hh.fs.Reset()
		hh.private(in)
		if got := hh.last().Msg.Text; got != i18n.Default("en").T("settings.remind.past") {
			t.Errorf("%s: got %q", in, got)
		}
	}
	if len(hh.svc.todos) != 1 || len(hh.svc.remindAt) != 0 {
		t.Fatal("nothing may be created")
	}
}
