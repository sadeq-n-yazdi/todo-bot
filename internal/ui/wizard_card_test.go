package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

const wizVer = 4242

// dump renders a message the way the fixtures store it: the text, then one
// line per keyboard row with [label|data] cells.
func dump(m tg.OutMessage) string {
	var b strings.Builder
	b.WriteString(m.Text)
	b.WriteString("\n---\n")
	for _, row := range m.Keyboard {
		for i, btn := range row {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString("[" + btn.Text + "|" + btn.Data + "]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// golden compares got with testdata/<name>; WIZARD_GOLDEN=update rewrites it.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("WIZARD_GOLDEN") == "update" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with WIZARD_GOLDEN=update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func wview(step model.DraftStep, d model.DraftData) WizardView {
	return WizardView{
		Draft:       model.Draft{ChatID: -100, UserID: 7, Step: step, Data: d, Version: wizVer},
		DefaultTime: "09:00",
		Members:     []model.User{{ID: 11, Username: "ali"}, {ID: 12, Username: "sara"}},
	}
}

func buttonsOf(m tg.OutMessage) (out []tg.Button) {
	for _, r := range m.Keyboard {
		out = append(out, r...)
	}
	return out
}

func hasData(m tg.OutMessage, data string) bool {
	for _, b := range buttonsOf(m) {
		if b.Data == data {
			return true
		}
	}
	return false
}

func nav(arg string) string { return Encode(Action{Kind: ActWizNav, ID: wizVer, Arg: arg}) }

// steps lists every card the wizard draws, with the nav buttons that the
// ARCHITECTURE §12 table gives it.
func wizardCases() []struct {
	name                string
	v                   WizardView
	skip, back, cancel  bool
	save, edit, yes, no bool
} {
	tom := time.Date(2026, 10, 14, 15, 0, 0, 0, testLoc).UTC()
	withDate := model.DraftData{Text: "buy <milk>", OrigText: "buy <milk>", Date: "2026-10-14", Time: "15:00", Repeat: model.RepeatWeekly}
	editMenu := wview(model.StepConfirm, withDate)
	editMenu.EditMenu = true
	grp := wview(model.StepAssignee, withDate)
	grp.Group = true
	noted := wview(model.StepText, model.DraftData{})
	noted.Note = "That time <has> passed"
	type C = struct {
		name                string
		v                   WizardView
		skip, back, cancel  bool
		save, edit, yes, no bool
	}
	return []C{
		{name: "text", v: wview(model.StepText, model.DraftData{}), cancel: true},
		{name: "found", v: wview(model.StepFound, model.DraftData{Text: "x", OrigText: "x tomorrow 15:00", FoundText: "x", FoundDue: &tom, FoundTime: true}), back: true, cancel: true, yes: true, no: true},
		{name: "date", v: wview(model.StepDate, model.DraftData{}), skip: true, back: true, cancel: true},
		{name: "time", v: wview(model.StepTime, model.DraftData{Date: "2026-10-14"}), skip: true, back: true, cancel: true},
		{name: "time_text", v: wview(model.StepTimeText, model.DraftData{Date: "2026-10-14"}), back: true, cancel: true},
		{name: "repeat", v: wview(model.StepRepeat, model.DraftData{Date: "2026-10-14", Time: "15:00"}), skip: true, back: true, cancel: true},
		{name: "repeat_last", v: wview(model.StepRepeat, model.DraftData{Date: "2026-10-30", Time: "16:00"}), skip: true, back: true, cancel: true},
		{name: "repeat_both", v: wview(model.StepRepeat, model.DraftData{Date: "2027-02-26", Time: "16:00"}), skip: true, back: true, cancel: true},
		{name: "confirm_nth", v: wview(model.StepConfirm, model.DraftData{Text: "x", Date: "2026-10-23", Time: "16:00", Repeat: model.RepeatNthWeekday}), save: true, edit: true, cancel: true},
		{name: "assignee_private", v: wview(model.StepAssignee, withDate), skip: true, back: true, cancel: true},
		{name: "assignee_group", v: grp, skip: true, back: true, cancel: true},
		{name: "confirm", v: wview(model.StepConfirm, withDate), save: true, edit: true, cancel: true},
		{name: "edit_menu", v: editMenu, back: true},
		{name: "note", v: noted, cancel: true},
	}
}

func TestWizardKeyboards(t *testing.T) {
	for _, lc := range []i18n.Locale{locEnG, locFaJ} {
		for _, c := range wizardCases() {
			t.Run(string(lc.Lang)+"/"+c.name, func(t *testing.T) {
				m := WizardCard(c.v, lc, testLoc, testNow)
				if m.Text == "" || len(m.Keyboard) == 0 {
					t.Fatalf("empty card %+v", m)
				}
				if m.ForceReply {
					t.Error("card must not carry ForceReply")
				}
				for _, b := range buttonsOf(m) {
					if b.Text == "" {
						t.Errorf("empty label for %q", b.Data)
					}
					if len(b.Data) > 64 {
						t.Errorf("data %q is %d bytes", b.Data, len(b.Data))
					}
					a, err := Decode(b.Data)
					if err != nil {
						t.Errorf("data %q does not decode: %v", b.Data, err)
						continue
					}
					if a.Kind != ActWizNoop && a.ID != wizVer {
						t.Errorf("data %q has ID %d, want the draft version", b.Data, a.ID)
					}
					if Encode(a) != b.Data {
						t.Errorf("data %q is not canonical", b.Data)
					}
				}
				for _, w := range []struct {
					arg  string
					want bool
				}{{WizNavSkip, c.skip}, {WizNavBack, c.back}, {WizNavCancel, c.cancel}, {WizNavSave, c.save}, {WizNavEdit, c.edit}, {WizNavUseFound, c.yes}, {WizNavNoFound, c.no}} {
					if got := hasData(m, nav(w.arg)); got != w.want {
						t.Errorf("nav %q present = %v, want %v", w.arg, got, w.want)
					}
				}
				golden(t, "wizard_card_"+c.name+"_"+string(lc.Lang)+".golden", dump(m))
			})
		}
	}
}

func TestWizardNoteIsEscaped(t *testing.T) {
	v := wview(model.StepText, model.DraftData{})
	v.Note = "That time <has> passed"
	m := WizardCard(v, locEnG, testLoc, testNow)
	if !strings.HasPrefix(m.Text, "That time &lt;has&gt; passed\n\n") {
		t.Fatalf("text %q", m.Text)
	}
}

func TestWizardUnknownStep(t *testing.T) {
	if m := WizardCard(wview("bogus", model.DraftData{}), locEnG, testLoc, testNow); m.Text != "" || m.Keyboard != nil {
		t.Fatalf("got %+v", m)
	}
}

func TestDateQuickPicks(t *testing.T) {
	m := WizardCard(wview(model.StepDate, model.DraftData{}), locEnG, testLoc, testNow)
	want := []string{
		"wd:4242:20261013", "wd:4242:20261014", // Today, Tomorrow
		"wd:4242:20261015", "wd:4242:20261016", "wd:4242:20261017", "wd:4242:20261018", "wd:4242:20261019",
		"wd:4242:o", "wd:4242:n",
	}
	var got []string
	for _, b := range buttonsOf(m)[:len(want)] {
		got = append(got, b.Data)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
	// weekday names by language; Farsi uses the user's calendar day number
	if !strings.Contains(dump(m), "[Thu 15|") {
		t.Errorf("no weekday button in %s", dump(m))
	}
	fa := WizardCard(wview(model.StepDate, model.DraftData{}), locFaJ, testLoc, testNow)
	if !strings.Contains(dump(fa), "[پنج\u200cشنبه ۲۳|wd:4242:20261015]") {
		t.Errorf("no Farsi weekday button in %s", dump(fa))
	}
}

func TestDateQuickPicksDSTSafe(t *testing.T) {
	ber, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata")
	}
	// The day before the clocks go back (25 Oct 2026): the pick after is 26 Oct.
	now := time.Date(2026, 10, 24, 23, 30, 0, 0, ber)
	m := WizardCard(wview(model.StepDate, model.DraftData{}), locEnG, ber, now)
	if !hasData(m, "wd:4242:20261025") || !hasData(m, "wd:4242:20261026") || !hasData(m, "wd:4242:20261030") {
		t.Fatalf("picks: %s", dump(m))
	}
}

func TestTimeStepButtons(t *testing.T) {
	m := WizardCard(wview(model.StepTime, model.DraftData{}), locFaJ, testLoc, testNow)
	for i, s := range WizardTimes {
		if !hasData(m, "wt:4242:"+strings.Replace(s, ":", "", 1)) {
			t.Errorf("no button for %s", s)
		}
		_ = i
	}
	if !hasData(m, "wt:4242:k") || !hasData(m, "wt:4242:d") {
		t.Error("no Type / Default button")
	}
	if !strings.Contains(dump(m), "۰۹:۰۰") {
		t.Errorf("Persian digits missing: %s", dump(m))
	}
}

func TestRepeatLabelsFollowTheDate(t *testing.T) {
	v := wview(model.StepRepeat, model.DraftData{Date: "2026-10-14", Time: "15:00"})
	en := dump(WizardCard(v, locEnG, testLoc, testNow))
	if !strings.Contains(en, "Weekly (Wednesday)") || !strings.Contains(en, "Monthly (day 14)") {
		t.Errorf("gregorian: %s", en)
	}
	// 14 Oct 2026 is 22 Mehr 1405.
	fa := dump(WizardCard(v, locFaJ, testLoc, testNow))
	if !strings.Contains(fa, "هفتگی (چهارشنبه)") || !strings.Contains(fa, "ماهانه (روز ۲۲)") {
		t.Errorf("jalali: %s", fa)
	}
	for _, arg := range []string{"o", "d", "w", "m", "y"} {
		if !hasData(WizardCard(v, locEnG, testLoc, testNow), "wr:4242:"+arg) {
			t.Errorf("no repeat %q", arg)
		}
	}
}

func TestAssigneeChoices(t *testing.T) {
	v := wview(model.StepAssignee, model.DraftData{})
	priv := WizardCard(v, locEnG, testLoc, testNow)
	for _, d := range []string{"wa:4242:m", "wa:4242:11", "wa:4242:12"} {
		if !hasData(priv, d) {
			t.Errorf("private: no %s", d)
		}
	}
	if hasData(priv, "wa:4242:n") {
		t.Error("private chat offers Nobody")
	}
	v.Group = true
	grp := WizardCard(v, locEnG, testLoc, testNow)
	if !hasData(grp, "wa:4242:n") || !hasData(grp, "wa:4242:m") {
		t.Error("group lacks Nobody or Me")
	}
	if !strings.Contains(dump(grp), "[@ali|wa:4242:11]") {
		t.Errorf("member label: %s", dump(grp))
	}
	// no members in a private chat: only Me
	v.Group, v.Members = false, nil
	if got := len(buttonsOf(WizardCard(v, locEnG, testLoc, testNow))); got != 1+3 {
		t.Errorf("buttons = %d", got)
	}
}

func TestEditMenuSteps(t *testing.T) {
	v := wview(model.StepConfirm, model.DraftData{Text: "x"})
	v.EditMenu = true
	m := WizardCard(v, locEnG, testLoc, testNow)
	for _, a := range []string{"text", "date", "time", "repeat", "assignee"} {
		if !hasData(m, "we:4242:"+a) {
			t.Errorf("no edit %s", a)
		}
	}
	if hasData(m, nav(WizNavSave)) {
		t.Error("edit menu offers Save")
	}
}

func withName(v WizardView, n string) WizardView { v.AssigneeName = n; return v }

func TestSummary(t *testing.T) {
	found := time.Date(2026, 10, 14, 9, 0, 0, 0, testLoc).UTC()
	cases := []struct {
		name  string
		lc    i18n.Locale
		v     WizardView
		wants []string
	}{
		{"en gregorian weekly", locEnG,
			withName(wview(model.StepConfirm, model.DraftData{Text: "buy <milk>", Date: "2026-10-14", Time: "15:00", Repeat: model.RepeatWeekly, AssigneeID: 11}), "ali"),
			[]string{"📝 buy &lt;milk&gt;", "📅 tomorrow 15:00", "🔁 Weekly", "👤 <a href=\"tg://user?id=11\">@ali</a>"}},
		{"fa jalali yearly, default time", locFaJ,
			wview(model.StepConfirm, model.DraftData{Text: "کتاب", Date: "2026-10-20", Repeat: model.RepeatYearly}),
			[]string{"📝 کتاب", "📅 سه\u200cشنبه ۲۸ مهر ۰۹:۰۰", "🔁 سالانه", "👤 من"}},
		{"en no date private", locEnG,
			wview(model.StepConfirm, model.DraftData{Text: "x"}),
			[]string{"📅 No date", "👤 Me"}},
		{"fa no date", locFaJ,
			wview(model.StepConfirm, model.DraftData{Text: "x"}),
			[]string{"📅 بدون تاریخ", "👤 من"}},
		{"found date with time", locEnG,
			wview(model.StepConfirm, model.DraftData{Text: "x", FoundText: "x", FoundDue: &found, FoundTime: true, FoundRule: "daily 09:00"}),
			[]string{"📅 tomorrow 09:00", "🔁 Repeats (from your text)"}},
		{"found date without time", locEnG,
			wview(model.StepConfirm, model.DraftData{Text: "x", FoundDue: &found}),
			[]string{"📅 tomorrow\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := WizardCard(c.v, c.lc, testLoc, testNow)
			for _, w := range c.wants {
				if !strings.Contains(m.Text+"\n", w) {
					t.Errorf("summary lacks %q:\n%s", w, m.Text)
				}
			}
		})
	}
	// The assignee label in a group.
	g := wview(model.StepConfirm, model.DraftData{Text: "x"})
	g.Group = true
	if m := WizardCard(g, locEnG, testLoc, testNow); !strings.Contains(m.Text, "👤 Nobody") {
		t.Errorf("group nobody: %s", m.Text)
	}
	g.Draft.Data.AssigneeID = g.Draft.UserID
	if m := WizardCard(g, locEnG, testLoc, testNow); !strings.Contains(m.Text, "👤 Me") {
		t.Errorf("group me: %s", m.Text)
	}
	// A repeat on a dateless item is not shown.
	n := wview(model.StepConfirm, model.DraftData{Text: "x", Repeat: model.RepeatDaily})
	if m := WizardCard(n, locEnG, testLoc, testNow); strings.Contains(m.Text, "🔁") {
		t.Errorf("repeat on dateless item: %s", m.Text)
	}
}

func TestWizardAsk(t *testing.T) {
	cases := []struct {
		step model.DraftStep
		lc   i18n.Locale
		want string
	}{
		{model.StepText, locEnG, "What's the task?"},
		{model.StepTimeText, locEnG, "Reply with a time: 15:30, 3pm"},
		{model.StepText, locFaJ, "کار چیست؟"},
		{model.StepTimeText, locFaJ, "۱۵:۳۰"},
	}
	for _, c := range cases {
		m := WizardAsk(c.step, c.lc)
		if !m.ForceReply || m.Keyboard != nil || !strings.Contains(m.Text, c.want) {
			t.Errorf("WizardAsk(%s, %s) = %+v", c.step, c.lc.Lang, m)
		}
	}
}

func TestWizardKeysInBothLanguages(t *testing.T) {
	en, fa := map[string]bool{}, map[string]bool{}
	for _, k := range i18n.Keys(i18n.En) {
		if strings.HasPrefix(k, "ui.wizard.") {
			en[k] = true
		}
	}
	for _, k := range i18n.Keys(i18n.Fa) {
		if strings.HasPrefix(k, "ui.wizard.") {
			fa[k] = true
		}
	}
	if len(en) == 0 || len(en) != len(fa) {
		t.Fatalf("en %d keys, fa %d keys", len(en), len(fa))
	}
	for k := range en {
		if !fa[k] {
			t.Errorf("%s missing in fa", k)
		}
	}
}

// keep the calendar import used by helpers shared with the grid tests
var _ = calendar.Gregorian

func TestAssigneeCap(t *testing.T) {
	v := wview(model.StepAssignee, model.DraftData{})
	v.Group = true
	v.Members = nil
	for i := 1; i <= 150; i++ {
		v.Members = append(v.Members, model.User{ID: int64(100 + i), Username: "u"})
	}
	m := WizardCard(v, locEnG, testLoc, testNow)
	if n := len(buttonsOf(m)); n > 90 {
		t.Fatalf("%d buttons", n)
	}
	for _, d := range []string{nav(WizNavSkip), nav(WizNavBack), nav(WizNavCancel), "wa:4242:n", "wa:4242:m", "wa:4242:101"} {
		if !hasData(m, d) {
			t.Errorf("missing %s", d)
		}
	}
}

func TestLongTextStaysUnderLimit(t *testing.T) {
	v := wview(model.StepConfirm, model.DraftData{Text: strings.Repeat("<", 1000), Date: "2026-10-14"})
	v.Note = strings.Repeat("&", 1000)
	m := WizardCard(v, locFaJ, testLoc, testNow)
	if len([]rune(m.Text)) > 3000 {
		t.Fatalf("text has %d runes", len([]rune(m.Text)))
	}
	if !strings.Contains(m.Text, "…") {
		t.Error("no ellipsis")
	}
}

func TestNthWeekdayChoices(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name         string
		cal          calendar.Kind
		t            time.Time
		nth          int
		hasNth, last bool
	}{
		{"greg 1st", calendar.Gregorian, d(2026, 10, 2), 1, true, false},
		{"greg 4th", calendar.Gregorian, d(2026, 10, 23), 4, true, false},
		{"greg 5th is last", calendar.Gregorian, d(2026, 10, 30), 5, false, true},
		{"greg day 28 of 31 is 4th and last", calendar.Gregorian, d(2026, 10, 28), 4, true, true},
		{"greg 28-day month: 4th and last", calendar.Gregorian, d(2027, 2, 26), 4, true, true},
		{"greg leap Feb 29 is 5th and last", calendar.Gregorian, d(2028, 2, 29), 5, false, true},
		// 1 Aban 1405 = 2026-10-23 (Friday); 29 Aban 1405 = 2026-11-20.
		{"jalali 1 Aban", calendar.Jalali, d(2026, 10, 23), 1, true, false},
		{"jalali 29 Aban is last", calendar.Jalali, d(2026, 11, 20), 5, false, true},
	}
	for _, c := range cases {
		nth, hasNth, last := NthWeekdayChoices(c.cal, c.t)
		if nth != c.nth || hasNth != c.hasNth || last != c.last {
			t.Errorf("%s: got %d %v %v, want %d %v %v", c.name, nth, hasNth, last, c.nth, c.hasNth, c.last)
		}
	}
}

func TestNthRepeatLabels(t *testing.T) {
	// 2026-10-30 is the last Friday; 2026-10-23 is the 4th Friday.
	last := dump(WizardCard(wview(model.StepRepeat, model.DraftData{Date: "2026-10-30"}), locEnG, testLoc, testNow))
	if !strings.Contains(last, "[Monthly, last Friday|wr:4242:l]") || strings.Contains(last, "wr:4242:n") {
		t.Errorf("last: %s", last)
	}
	fourth := dump(WizardCard(wview(model.StepRepeat, model.DraftData{Date: "2026-10-23"}), locEnG, testLoc, testNow))
	if !strings.Contains(fourth, "[Monthly, 4th Friday|wr:4242:n]") || strings.Contains(fourth, "wr:4242:l") {
		t.Errorf("4th: %s", fourth)
	}
	// 1 Aban 1405 (2026-10-23) in Farsi; 29 Aban 1405 (2026-11-20) is the last.
	fa := dump(WizardCard(wview(model.StepRepeat, model.DraftData{Date: "2026-11-20"}), locFaJ, testLoc, testNow))
	if !strings.Contains(fa, "[ماهانه، آخرین جمعه|wr:4242:l]") {
		t.Errorf("fa last: %s", fa)
	}
	fa = dump(WizardCard(wview(model.StepRepeat, model.DraftData{Date: "2026-10-23"}), locFaJ, testLoc, testNow))
	if !strings.Contains(fa, "[ماهانه، اولین جمعه|wr:4242:n]") {
		t.Errorf("fa 1st: %s", fa)
	}
	// Summary shows the label; no date -> no extra buttons.
	sum := WizardCard(wview(model.StepConfirm, model.DraftData{Text: "x", Date: "2026-10-30", Repeat: model.RepeatLastWeekday}), locEnG, testLoc, testNow)
	if !strings.Contains(sum.Text, "Monthly, last Friday") {
		t.Errorf("summary: %s", sum.Text)
	}
	none := dump(WizardCard(wview(model.StepRepeat, model.DraftData{}), locEnG, testLoc, testNow))
	if strings.Contains(none, "wr:4242:n") || strings.Contains(none, "wr:4242:l") {
		t.Errorf("no date: %s", none)
	}
}

func TestNthRepeatLabelsFitButtons(t *testing.T) {
	for _, lc := range []i18n.Locale{locEnG, locFaJ} {
		for day := 1; day <= 31; day++ {
			v := wview(model.StepRepeat, model.DraftData{Date: time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")})
			for _, b := range buttonsOf(WizardCard(v, lc, testLoc, testNow)) {
				if b.Text == "" || len([]rune(b.Text)) > 40 {
					t.Errorf("%s day %d: label %q", lc.Lang, day, b.Text)
				}
			}
		}
	}
}
