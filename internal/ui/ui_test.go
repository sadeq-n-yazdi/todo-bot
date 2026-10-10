package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func FuzzEncodeDecode(f *testing.F) {
	f.Add("td", int64(1), "")
	f.Add("rs", int64(-9223372036854775808), "10m")
	f.Add("lp", int64(0), "12")
	f.Add("rs", int64(9223372036854775807), strings.Repeat("x", 200))
	f.Add("lp:7:3", int64(7), "3")
	kinds := []ActionKind{ActDone, ActDelete, ActDeleteConfirm, ActEdit, ActRemind, ActPage, ActReminderDone, ActSnooze, ActCancel}
	f.Fuzz(func(t *testing.T, kind string, id int64, arg string) {
		if got, err := Decode(kind); err == nil && Encode(got) != kind {
			t.Fatalf("Decode(%q)=%+v re-encodes to %q", kind, got, Encode(got))
		}
		a := Action{Kind: ActionKind(kind), ID: id, Arg: arg}
		if len(Encode(a)) > 64 {
			t.Fatalf("Encode(%+v) too long", a)
		}
		for _, k := range kinds {
			a := Action{Kind: k, ID: id, Arg: arg}
			s := Encode(a)
			if len(s) > 64 {
				t.Fatalf("Encode(%+v)=%q exceeds 64 bytes", a, s)
			}
			if !validArg(k, arg) && (k == ActSnooze || k == ActPage) {
				continue
			}
			want := a
			if k != ActSnooze && k != ActPage {
				want.Arg = ""
			}
			if k == ActPage {
				want.ID = 0
			}
			got, err := Decode(s)
			if err != nil || got != want {
				t.Fatalf("Decode(%q) = %+v, %v; want %+v", s, got, err, want)
			}
		}
	})
}

func TestEncodeDecode(t *testing.T) {
	cases := []struct {
		a Action
		s string
	}{
		{Action{Kind: ActDone, ID: 123}, "td:123"},
		{Action{Kind: ActSnooze, ID: 45, Arg: Snooze1h}, "rs:45:1h"},
		{Action{Kind: ActPage, Arg: "3"}, "lp:0:3"},
	}
	for _, c := range cases {
		if got := Encode(c.a); got != c.s {
			t.Errorf("Encode(%+v)=%q want %q", c.a, got, c.s)
		}
		if got, err := Decode(c.s); err != nil || got != c.a {
			t.Errorf("Decode(%q)=%+v,%v want %+v", c.s, got, err, c.a)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, s := range []string{"", "td", "td:", "td:x", "td:01", "td:+1", "zz:1", "td:1:2", "td:1:", "rs:1", "rs:1:5m", "rs:1:10m:x", "lp:0", "lp:0:-1", "lp:0:x", "lp:7:3", "td:99999999999999999999"} {
		if _, err := Decode(s); !errors.Is(err, ErrBadAction) {
			t.Errorf("Decode(%q) err=%v, want ErrBadAction", s, err)
		}
	}
}

func TestEscape(t *testing.T) {
	if got := Escape(`a & <b> "c"`); got != `a &amp; &lt;b&gt; "c"` {
		t.Errorf("got %q", got)
	}
}

func TestFormatWhen(t *testing.T) {
	nextYear := time.Date(2027, 10, 14, 9, 0, 0, 0, testLoc)
	cases := []struct {
		name string
		t    time.Time
		lc   i18n.Locale
		want string
	}{
		{"today en", *at(13, 18, 0), locEnG, "today 18:00"},
		{"tomorrow en", *at(14, 9, 0), locEnG, "tomorrow 09:00"},
		{"this week en", *at(17, 9, 0), locEnG, "Sat 17 Oct 09:00"},
		{"later same year en", *at(30, 9, 0), locEnG, "Fri 30 Oct 09:00"},
		{"next year en", nextYear, locEnG, "14 Oct 2027 09:00"},
		{"today fa", *at(13, 18, 0), locFaJ, "امروز ۱۸:۰۰"},
		{"tomorrow fa", *at(14, 9, 0), locFaJ, "فردا ۰۹:۰۰"},
		{"this week fa", *at(17, 9, 0), locFaJ, "شنبه ۲۵ مهر ۰۹:۰۰"},
		{"later same year fa", *at(30, 9, 0), locFaJ, "جمعه ۸ آبان ۰۹:۰۰"},
		{"next year fa", nextYear, locFaJ, "۲۲ مهر ۱۴۰۶ ۰۹:۰۰"},
		{"today en jalali", *at(13, 18, 0), locEnJ, "today 18:00"},
		{"this week en jalali", *at(17, 9, 0), locEnJ, "Sat 25 Mehr 09:00"},
		{"later same year en jalali", *at(30, 9, 0), locEnJ, "Fri 8 Aban 09:00"},
		{"next year en jalali", nextYear, locEnJ, "22 Mehr 1406 09:00"},
		{"timezone date, not UTC date", time.Date(2026, 10, 13, 22, 0, 0, 0, time.UTC), locEnG, "tomorrow 01:30"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatWhen(c.t, c.lc, testLoc, testNow); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestGoldenTodoList(t *testing.T) {
	cases := []struct {
		lc   i18n.Locale
		want string
	}{
		{locEnG, "📋 Your todos (1/2)\n3. buy &lt;milk&gt; &amp; eggs — today 18:00 🔁\n4. call mom — Mon 12 Oct 09:00 ⚠️\n12. no date"},
		{locFaJ, "📋 کارهای شما (۱/۲)\n۳. buy &lt;milk&gt; &amp; eggs — امروز ۱۸:۰۰ 🔁\n۴. call mom — دوشنبه ۲۰ مهر ۰۹:۰۰ ⚠️\n۱۲. no date"},
		{locEnJ, "📋 Your todos (1/2)\n3. buy &lt;milk&gt; &amp; eggs — today 18:00 🔁\n4. call mom — Mon 20 Mehr 09:00 ⚠️\n12. no date"},
	}
	for _, c := range cases {
		m := TodoList(sampleTodos(), c.lc, testLoc, testNow, 0, 2)
		if m.Text != c.want {
			t.Errorf("%v text:\n got %q\nwant %q", c.lc, m.Text, c.want)
		}
		if len(m.Keyboard) != 4 {
			t.Fatalf("rows=%d want 4", len(m.Keyboard))
		}
		row := m.Keyboard[0]
		wantData := []string{"td:101", "te:101", "tr:101", "tx:101"}
		for i, b := range row {
			if b.Data != wantData[i] {
				t.Errorf("button %d data=%q want %q", i, b.Data, wantData[i])
			}
		}
		if row[0].Text != "✅ "+c.lc.Num(3) || row[1].Text != "✏️" || row[2].Text != "⏰" || row[3].Text != "🗑" {
			t.Errorf("row labels: %+v", row)
		}
		if paging := m.Keyboard[3]; len(paging) != 1 || paging[0] != (tg.Button{Text: "›", Data: "lp:0:1"}) {
			t.Errorf("page 0 paging row: %+v", paging)
		}
	}
}

func TestTodoListPaging(t *testing.T) {
	ts := sampleTodos()[:1]
	last := TodoList(ts, locEnG, testLoc, testNow, 2, 3).Keyboard
	if got := last[len(last)-1]; len(got) != 1 || got[0] != (tg.Button{Text: "‹", Data: "lp:0:1"}) {
		t.Errorf("last page row: %+v", got)
	}
	mid := TodoList(ts, locEnG, testLoc, testNow, 1, 3).Keyboard
	if got := mid[len(mid)-1]; len(got) != 2 || got[0].Data != "lp:0:0" || got[1].Data != "lp:0:2" {
		t.Errorf("middle page row: %+v", got)
	}
	single := TodoList(ts, locEnG, testLoc, testNow, 0, 1)
	if len(single.Keyboard) != 1 || strings.Contains(single.Text, "(1/1)") {
		t.Errorf("single page should have no paging: %+v", single)
	}
}

func TestTodoListEmpty(t *testing.T) {
	cases := map[string]i18n.Locale{"Nothing to do 🎉": locEnG, "کاری نمانده 🎉": locFaJ}
	for want, lc := range cases {
		m := TodoList(nil, lc, testLoc, testNow, 0, 1)
		if m.Text != want || len(m.Keyboard) != 0 {
			t.Errorf("%+v", m)
		}
	}
}

func TestGoldenReminder(t *testing.T) {
	cases := []struct {
		lc                i18n.Locale
		onTime, late      string
		done, m10, h1, tm string
	}{
		{locEnG, "⏰ pay &lt;rent&gt;", "⏰ pay &lt;rent&gt;\nlate — was due today 09:00", "✅ Done", "10m", "1h", "Tomorrow"},
		{locFaJ, "⏰ pay &lt;rent&gt;", "⏰ pay &lt;rent&gt;\nبا تأخیر — موعد: امروز ۰۹:۰۰", "✅ انجام شد", "۱۰ دقیقه", "۱ ساعت", "فردا"},
		{locEnJ, "⏰ pay &lt;rent&gt;", "⏰ pay &lt;rent&gt;\nlate — was due today 09:00", "✅ Done", "10m", "1h", "Tomorrow"},
	}
	for _, c := range cases {
		m := ReminderMsg(sampleReminder(), c.lc, testLoc, testNow, false)
		if m.Text != c.onTime {
			t.Errorf("on time: got %q want %q", m.Text, c.onTime)
		}
		if got := ReminderMsg(sampleReminder(), c.lc, testLoc, testNow, true).Text; got != c.late {
			t.Errorf("late: got %q want %q", got, c.late)
		}
		want := [][]tg.Button{
			{{Text: c.done, Data: "rd:45"}},
			{{Text: c.m10, Data: "rs:45:10m"}, {Text: c.h1, Data: "rs:45:1h"}, {Text: c.tm, Data: "rs:45:tm"}},
		}
		if len(m.Keyboard) != 2 || len(m.Keyboard[1]) != 3 {
			t.Fatalf("keyboard shape: %+v", m.Keyboard)
		}
		for i := range want {
			for j := range want[i] {
				if m.Keyboard[i][j] != want[i][j] {
					t.Errorf("button [%d][%d]=%+v want %+v", i, j, m.Keyboard[i][j], want[i][j])
				}
			}
		}
	}
}

func TestGoldenDigest(t *testing.T) {
	td := sampleTodos()
	cases := []struct {
		lc          i18n.Locale
		full, empty string
	}{
		{locEnG, "<b>⚠️ Overdue</b>\n4. call mom — Mon 12 Oct 09:00 ⚠️\n\n<b>📅 Today</b>\n3. buy &lt;milk&gt; &amp; eggs — today 18:00 🔁\n\n+12 without date", "Nothing due today 🎉"},
		{locFaJ, "<b>⚠️ عقب\u200cافتاده</b>\n۴. call mom — دوشنبه ۲۰ مهر ۰۹:۰۰ ⚠️\n\n<b>📅 امروز</b>\n۳. buy &lt;milk&gt; &amp; eggs — امروز ۱۸:۰۰ 🔁\n\n+۱۲ بدون تاریخ", "امروز کاری نداری 🎉"},
		{locEnJ, "<b>⚠️ Overdue</b>\n4. call mom — Mon 20 Mehr 09:00 ⚠️\n\n<b>📅 Today</b>\n3. buy &lt;milk&gt; &amp; eggs — today 18:00 🔁\n\n+12 without date", "Nothing due today 🎉"},
	}
	for _, c := range cases {
		v := model.DigestView{Locale: c.lc, Loc: testLoc, Overdue: td[1:2], Today: td[:1], NoDateCount: 12}
		if got := Digest(v, testNow).Text; got != c.full {
			t.Errorf("full:\n got %q\nwant %q", got, c.full)
		}
		if got := Digest(model.DigestView{Locale: c.lc, Loc: testLoc}, testNow).Text; got != c.empty {
			t.Errorf("empty: got %q want %q", got, c.empty)
		}
	}
}

func TestCallbackDataLength(t *testing.T) {
	for _, m := range []tg.OutMessage{
		TodoList(sampleTodos(), locEnG, testLoc, testNow, 5, 9),
		ReminderMsg(sampleReminder(), locEnG, testLoc, testNow, false),
	} {
		for _, row := range m.Keyboard {
			for _, b := range row {
				if _, err := Decode(b.Data); err != nil || len(b.Data) > 64 {
					t.Errorf("button %+v: err=%v", b, err)
				}
			}
		}
	}
}

func TestMessagesComplete(t *testing.T) {
	fa := map[string]bool{}
	for _, k := range i18n.Keys(i18n.Fa) {
		fa[k] = true
	}
	n := 0
	for _, k := range i18n.Keys(i18n.En) {
		if !strings.HasPrefix(k, "ui.") {
			continue
		}
		n++
		if !fa[k] {
			t.Errorf("key %s has no Farsi entry", k)
		}
	}
	if n == 0 {
		t.Error("no ui.* keys registered")
	}
}
