package e2e

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Wizard scenarios (#68, ARCHITECTURE §12). Toasts and card text are
// compared through the i18n keys (Locale.T), never as literal strings.
// The fake clock starts on Saturday 2026-10-10 12:00 UTC.

var wizardNames = map[int64]string{alice: dadName, bob: saraName}

func wizardStart() time.Time { return utc(2026, 10, 10, 12, 0) }

func newWizardStack(t *testing.T) *stack {
	t.Helper()
	return newStack(t, dbURL(t.TempDir()), clocktest.New(wizardStart()), stackOpts{})
}

// wsay delivers a text message with the sender's @username, as a reply to
// replyTo if not 0.
func (s *stack) wsay(from, chat int64, text string, replyTo int) {
	s.t.Helper()
	kind, title := model.ChatPrivate, ""
	if chat < 0 {
		kind, title = model.ChatGroup, "Family"
	}
	id := updateSeq.Add(1)
	s.deliver(tg.Update{ID: id, Message: &tg.Message{
		ID: int(id), ChatID: chat, ChatKind: kind, ChatTitle: title,
		FromID: from, FromName: "u", FromUsername: wizardNames[from], Text: text, ReplyToID: replyTo,
	}})
}

// locale returns the stored locale of a user.
func (s *stack) locale(user int64) i18n.Locale {
	s.t.Helper()
	p, err := s.svc.Prefs(context.Background(), user, "")
	if err != nil {
		s.t.Fatalf("prefs: %v", err)
	}
	return p.Locale
}

func (s *stack) draft(chat, user int64) (model.Draft, error) {
	return s.svc.Draft(context.Background(), chat, user)
}

func (s *stack) mustDraft(chat, user int64) model.Draft {
	s.t.Helper()
	d, err := s.draft(chat, user)
	if err != nil {
		s.t.Fatalf("draft of %d in %d: %v", user, chat, err)
	}
	return d
}

func (s *stack) noDraft(chat, user int64) {
	s.t.Helper()
	if d, err := s.draft(chat, user); !errors.Is(err, service.ErrNotFound) {
		s.t.Fatalf("draft of %d in %d = %+v (err %v), want none", user, chat, d, err)
	}
}

func (s *stack) todos(chat int64) []model.Todo {
	s.t.Helper()
	l, err := s.st.ListTodos(context.Background(), model.TodoQuery{ChatID: chat})
	if err != nil {
		s.t.Fatalf("list todos: %v", err)
	}
	return l
}

// newest returns the last non-edit message sent to chat for which f holds.
func (s *stack) newest(chat int64, f func(tgtest.Sent) bool) (tgtest.Sent, bool) {
	ms := s.send.Messages()
	for i := len(ms) - 1; i >= 0; i-- {
		if !ms[i].Edit && ms[i].ChatID == chat && f(ms[i]) {
			return ms[i], true
		}
	}
	return tgtest.Sent{}, false
}

// card returns the ID of the newest wizard card in chat.
func (s *stack) card(chat int64) int {
	s.t.Helper()
	m, ok := s.newest(chat, func(m tgtest.Sent) bool {
		if len(m.Msg.Keyboard) == 0 || len(m.Msg.Keyboard[0]) == 0 {
			return false
		}
		a, err := ui.Decode(m.Msg.Keyboard[0][0].Data)
		return err == nil && a.Kind == ui.ActWizNoop
	})
	if !ok {
		s.t.Fatalf("no wizard card was sent to chat %d", chat)
	}
	return m.MsgID
}

// asks returns the ForceReply questions sent to chat.
func (s *stack) asks(chat int64) []tgtest.Sent {
	var out []tgtest.Sent
	for _, m := range s.send.Messages() {
		if !m.Edit && m.ChatID == chat && m.Msg.ForceReply {
			out = append(out, m)
		}
	}
	return out
}

// shown returns the latest version of message id in chat (a send or an edit).
func (s *stack) shown(chat int64, id int) tgtest.Sent {
	s.t.Helper()
	ms := s.send.Messages()
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].ChatID == chat && ms[i].MsgID == id {
			return ms[i]
		}
	}
	s.t.Fatalf("message %d was never sent to chat %d", id, chat)
	return tgtest.Sent{}
}

func (s *stack) edits(chat int64, id int) int {
	n := 0
	for _, m := range s.send.Messages() {
		if m.Edit && m.ChatID == chat && m.MsgID == id {
			n++
		}
	}
	return n
}

// wpress presses the button of card id (as it shows now) with kind and arg,
// as user. It returns the toast of the answer ("" if none).
func (s *stack) wpress(from, chat int64, id int, kind ui.ActionKind, arg string) string {
	s.t.Helper()
	data := buttonData(s.t, s.shown(chat, id), kind, arg)
	before := len(s.send.Answers)
	s.deliver(tg.Update{ID: updateSeq.Add(1), Callback: &tg.Callback{
		ID: "cb-" + strconv.FormatInt(updateSeq.Load(), 10), FromID: from, FromUsername: wizardNames[from],
		ChatID: chat, MessageID: id, Data: data,
	}})
	if len(s.send.Answers) > before {
		return s.send.Answers[len(s.send.Answers)-1].Text
	}
	return ""
}

func dayArg(y int, m time.Month, d int) string {
	return ui.DateArg(calendar.Date{Year: y, Month: int(m), Day: d})
}

// ---- entry points ----

func TestWizardEntryPoints(t *testing.T) {
	for _, lang := range []string{"en", "fa"} {
		t.Run(lang, func(t *testing.T) {
			s := newWizardStack(t)
			if lang == "fa" {
				s.say(alice, alice, "/lang fa")
			} else {
				s.say(alice, alice, "/lang en")
			}
			lc := s.locale(alice)
			if (lang == "fa") != (lc.Lang == i18n.Fa) {
				t.Fatalf("locale %+v for %s", lc, lang)
			}
			other := i18n.Locale{Lang: i18n.En}
			if lang == "en" {
				other = i18n.Locale{Lang: i18n.Fa}
			}
			s.say(alice, alice, "/add first") // gives /list a button to press
			listed := len(s.send.Messages())
			s.say(alice, alice, "/list")
			list, ok := s.newest(alice, func(tgtest.Sent) bool { return true })
			if !ok || len(s.send.Messages()) == listed {
				t.Fatal("/list sent nothing")
			}

			entries := []struct {
				name  string
				start func()
			}{
				{"/new", func() { s.say(alice, alice, "/new") }},
				{"bare /add", func() { s.say(alice, alice, "/add") }},
				{"list button", func() {
					d := buttonData(t, list, ui.ActWizStart, "")
					s.press(alice, alice, list.MsgID, d)
				}},
				{"reply keyboard label", func() { s.say(alice, alice, ui.NewTaskLabel(lc)) }},
				{"label in the other language", func() { s.say(alice, alice, ui.NewTaskLabel(other)) }},
			}
			prev := 0
			for _, e := range entries {
				before := len(s.todos(alice))
				e.start()
				card := s.card(alice)
				if card == prev {
					t.Fatalf("%s: no new card", e.name)
				}
				prev = card
				d := s.mustDraft(alice, alice)
				if d.Step != model.StepText || d.MsgID != card {
					t.Fatalf("%s: draft %+v, want the text step on card %d", e.name, d, card)
				}
				if got := s.shown(alice, card).Msg.Text; !strings.Contains(got, lc.T("ui.wizard.q.text")) {
					t.Errorf("%s: card %q does not ask %q", e.name, got, lc.T("ui.wizard.q.text"))
				}
				if n := len(s.todos(alice)); n != before {
					t.Errorf("%s: created %d items", e.name, n-before)
				}
			}
		})
	}
}

// ---- equivalence with /add ----

// A full path in a group gives the same item as the one-line command.
func TestWizardFullPathEqualsAddCommand(t *testing.T) {
	s := newWizardStack(t)
	s.sayDad(group, "/start")
	s.saySara(bob, "/start") // sara_home's username is learned
	const task = "buy milk"

	s.wsay(alice, group, "/new", 0)
	card := s.card(group)
	asks := s.asks(group)
	if len(asks) != 1 || asks[0].Msg.ReplyTo != card {
		t.Fatalf("questions %+v, want one ForceReply as a reply to card %d", asks, card)
	}
	s.wsay(alice, group, task, asks[0].MsgID)
	tmrw := dayArg(2026, 10, 11)
	s.wpress(alice, group, card, ui.ActWizDate, tmrw)
	s.wpress(alice, group, card, ui.ActWizTime, "1500")
	s.wpress(alice, group, card, ui.ActWizRepeat, ui.WizRepeatWeekly)
	s.wpress(alice, group, card, ui.ActWizAssign, strconv.FormatInt(bob, 10))
	if d := s.mustDraft(group, alice); d.Step != model.StepConfirm || d.Data.AssigneeID != bob {
		t.Fatalf("draft before Save: %+v", d)
	}
	s.wpress(alice, group, card, ui.ActWizNav, ui.WizNavSave)
	s.noDraft(group, alice)

	s.wsay(alice, group, "/add @"+saraName+" "+task+" tomorrow 15:00 every week", 0)

	l := s.todos(group)
	if len(l) != 2 {
		t.Fatalf("%d items in the group, want 2: %+v", len(l), l)
	}
	w, a := s.todoByNum(group, 1), s.todoByNum(group, 2)
	if w.Text != task || w.AssigneeID != bob || w.Recur != "weekly sun 15:00" {
		t.Fatalf("wizard item %+v", w)
	}
	if !w.DueAt.Equal(utc(2026, 10, 11, 15, 0)) {
		t.Errorf("wizard due %v", w.DueAt)
	}
	checks := []struct {
		field    string
		got, exp any
	}{
		{"Text", w.Text, a.Text},
		{"Status", w.Status, a.Status},
		{"ChatID", w.ChatID, a.ChatID},
		{"Recur", w.Recur, a.Recur},
		{"TZ", w.TZ, a.TZ},
		{"CreatedBy", w.CreatedBy, a.CreatedBy},
		{"AssigneeID", w.AssigneeID, a.AssigneeID},
		{"AssigneeName", w.AssigneeName, a.AssigneeName},
		{"DueAt", w.DueAt.UTC(), a.DueAt.UTC()},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.exp) {
			t.Errorf("%s: wizard %v, /add %v", c.field, c.got, c.exp)
		}
	}
	// The card now shows the same line /add would have printed.
	lc := s.locale(alice)
	if got := s.shown(group, card).Msg.Text; !strings.Contains(got, task) || len(s.shown(group, card).Msg.Keyboard) != 0 {
		t.Errorf("saved card %q (%s)", got, lc.T("todo.added", "1", ""))
	}
}

// ---- reply keyboard ----

func TestWizardReplyKeyboardOnce(t *testing.T) {
	s := newWizardStack(t)
	lc := s.locale(alice)
	if p, _ := s.svc.Prefs(context.Background(), alice, ""); p.KBVersion != 0 {
		t.Fatalf("new user has kb_version %d", p.KBVersion)
	}
	withKB := func(chat int64) []tgtest.Sent {
		var out []tgtest.Sent
		for _, m := range s.send.Messages() {
			if m.ChatID == chat && m.Msg.ReplyKeyboard != nil {
				out = append(out, m)
			}
		}
		return out
	}

	s.say(alice, alice, "/add first thing")
	got := withKB(alice)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Msg.ReplyKeyboard, ui.NewTaskKeyboard(lc)) {
		t.Fatalf("first reply: keyboards %+v, want one %v", got, ui.NewTaskKeyboard(lc))
	}
	u, err := s.st.GetUser(context.Background(), alice)
	if err != nil || u.KBVersion != ui.NewTaskKeyboardVersion {
		t.Fatalf("users.kb_version = %d (err %v), want %d", u.KBVersion, err, ui.NewTaskKeyboardVersion)
	}

	s.say(alice, alice, "/add second thing")
	s.say(alice, alice, "/list")
	s.say(alice, alice, "/new")
	s.say(alice, alice, "plain text of the wizard")
	if n := len(withKB(alice)); n != 1 {
		t.Fatalf("%d messages carry the keyboard, want only the first", n)
	}

	// Never in a group.
	s.sayDad(group, "/add in the group")
	if n := len(withKB(group)); n != 0 {
		t.Errorf("%d group messages carry the keyboard", n)
	}

	// A new language needs a new keyboard.
	s.say(alice, alice, "/lang fa")
	fa := s.locale(alice)
	got = withKB(alice)
	if len(got) != 2 || !reflect.DeepEqual(got[1].Msg.ReplyKeyboard, ui.NewTaskKeyboard(fa)) {
		t.Fatalf("after /lang fa: keyboards %+v", got)
	}
}

// ---- Jalali ----

func TestWizardJalaliMonthlyAndYearly(t *testing.T) {
	s := newWizardStack(t)
	s.say(alice, alice, "/lang fa")
	s.say(alice, alice, "/calendar jalali")
	lc := s.locale(alice)
	if lc.Cal != calendar.Jalali {
		t.Fatalf("locale %+v is not Jalali", lc)
	}
	tmr := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	jd := calendar.Jalali.Split(tmr)

	wizard := func(text, repeat string) model.Todo {
		before := len(s.todos(alice))
		s.say(alice, alice, "/new")
		card := s.card(alice)
		s.say(alice, alice, text)
		s.wpress(alice, alice, card, ui.ActWizDate, dayArg(2026, 10, 11))
		s.wpress(alice, alice, card, ui.ActWizTime, "0900")
		s.wpress(alice, alice, card, ui.ActWizRepeat, repeat)
		s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSave)
		l := s.todos(alice)
		if len(l) != before+1 {
			t.Fatalf("%d items, want %d", len(l), before+1)
		}
		return s.todoByNum(alice, before+1)
	}
	byText := func(line string) model.Todo {
		before := len(s.todos(alice))
		s.say(alice, alice, "/add "+line)
		if len(s.todos(alice)) != before+1 {
			t.Fatalf("/add %q created no item", line)
		}
		return s.todoByNum(alice, before+1)
	}
	rule := func(td model.Todo) recur.Rule {
		r, err := recur.Parse(td.Recur)
		if err != nil {
			t.Fatalf("rule %q: %v", td.Recur, err)
		}
		return r
	}

	m := wizard("monthly wizard", ui.WizRepeatMonthly)
	if r := rule(m); r.Freq != recur.Monthly || r.Cal != calendar.Jalali || r.MonthDay != jd.Day {
		t.Errorf("monthly rule %q, want Jalali day %d", m.Recur, jd.Day)
	}
	mt := byText("monthly text tomorrow 09:00 every month")
	if m.Recur != mt.Recur || !m.DueAt.Equal(*mt.DueAt) {
		t.Errorf("monthly: wizard %q %v, text %q %v", m.Recur, m.DueAt, mt.Recur, mt.DueAt)
	}

	y := wizard("yearly wizard", ui.WizRepeatYearly)
	if r := rule(y); r.Freq != recur.Yearly || r.Cal != calendar.Jalali || r.Month != jd.Month || r.MonthDay != jd.Day {
		t.Errorf("yearly rule %q, want Jalali %d/%d", y.Recur, jd.Month, jd.Day)
	}
	for _, line := range []string{"yearly text tomorrow 09:00 every year", "yearly farsi tomorrow 09:00 هر سال"} {
		yt := byText(line)
		if y.Recur != yt.Recur || !y.DueAt.Equal(*yt.DueAt) {
			t.Errorf("yearly %q: wizard %q %v, text %q %v", line, y.Recur, y.DueAt, yt.Recur, yt.DueAt)
		}
	}
}

// A date picked from the Jalali month grid, in the next month.
func TestWizardJalaliGridYearly(t *testing.T) {
	s := newWizardStack(t)
	s.say(alice, alice, "/lang fa")
	s.say(alice, alice, "/calendar jalali")
	now := calendar.Jalali.Split(wizardStart())
	s.say(alice, alice, "/new")
	card := s.card(alice)
	s.say(alice, alice, "grid task")
	s.wpress(alice, alice, card, ui.ActWizDate, ui.WizDateGrid)

	ny, nm := calendar.AddMonths(now.Year, now.Month, 1)
	next := ui.MonthArg(calendar.Jalali, ny, nm)
	s.wpress(alice, alice, card, ui.ActWizMonth, next)

	var pick string
	for _, row := range s.shown(alice, card).Msg.Keyboard {
		for _, b := range row {
			a, err := ui.Decode(b.Data)
			if err != nil || a.Kind != ui.ActWizDate {
				continue
			}
			if d, ok := ui.ParseDateArg(a.Arg); ok {
				y, mo, day := d.Year, time.Month(d.Month), d.Day
				if jd := calendar.Jalali.Split(time.Date(y, mo, day, 12, 0, 0, 0, time.UTC)); jd.Month == nm && jd.Day == 10 {
					pick = a.Arg
				}
			}
		}
	}
	if pick == "" {
		t.Fatalf("no button for day 10 of Jalali month %d in the grid", nm)
	}
	s.wpress(alice, alice, card, ui.ActWizDate, pick)
	s.wpress(alice, alice, card, ui.ActWizTime, "0900")
	s.wpress(alice, alice, card, ui.ActWizRepeat, ui.WizRepeatYearly)
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSave)

	td := s.todoByNum(alice, 1)
	r, err := recur.Parse(td.Recur)
	if err != nil || r.Freq != recur.Yearly || r.Cal != calendar.Jalali || r.Month != nm || r.MonthDay != 10 {
		t.Fatalf("rule %q (%v), want yearly Jalali %d/10", td.Recur, err, nm)
	}
	if got := calendar.Jalali.Split(td.DueAt.UTC()); got.Month != nm || got.Day != 10 || td.DueAt.UTC().Hour() != 9 {
		t.Errorf("due %v is Jalali %v", td.DueAt, got)
	}
}

// ---- expiry ----

func TestWizardExpiresAfter30Minutes(t *testing.T) {
	s := newWizardStack(t)
	lc := s.locale(alice)

	// 29 minutes: the card still works. Each answer extends the draft.
	s.say(alice, alice, "/new")
	card := s.card(alice)
	s.clk.Advance(29 * time.Minute)
	s.say(alice, alice, "slow task")
	if d := s.mustDraft(alice, alice); d.Data.Text != "slow task" {
		t.Fatalf("draft %+v", d)
	}
	s.clk.Advance(29 * time.Minute) // 58 minutes in all, 29 since the answer
	if toast := s.wpress(alice, alice, card, ui.ActWizDate, ui.WizDateNone); toast == lc.T("wizard.toast.expired") {
		t.Fatal("the draft expired although it was answered 29 minutes ago")
	}

	// 31 minutes without an answer: gone.
	s.clk.Advance(31 * time.Minute)
	editsBefore := s.edits(alice, card)
	toast := s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSave)
	if toast != lc.T("wizard.toast.expired") {
		t.Errorf("toast %q, want %q", toast, lc.T("wizard.toast.expired"))
	}
	if n := len(s.todos(alice)); n != 0 {
		t.Errorf("%d items were created", n)
	}
	s.noDraft(alice, alice)
	if n := s.edits(alice, card); n != editsBefore {
		t.Errorf("the card was edited %d times after expiry", n-editsBefore)
	}

	// A text now is an ordinary item.
	s.say(alice, alice, "ordinary item")
	if l := s.todos(alice); len(l) != 1 || l[0].Text != "ordinary item" {
		t.Errorf("items %+v, want the ordinary one", l)
	}
}

// ---- group rules ----

func TestWizardGroupRules(t *testing.T) {
	s := newWizardStack(t)
	s.sayDad(group, "/start")
	s.saySara(bob, "/start")
	bobLc := s.locale(bob)

	s.wsay(alice, group, "/new", 0)
	card := s.card(group)
	ask := s.asks(group)[0]
	if !ask.Msg.ForceReply || ask.Msg.ReplyTo != card {
		t.Fatalf("question %+v", ask.Msg)
	}
	sentBefore := len(s.send.Messages())

	// A text that is not a reply is ignored, as is another member's reply.
	s.wsay(alice, group, "chatter that is not a reply", 0)
	s.wsay(bob, group, "bob answers for alice", ask.MsgID)
	if d := s.mustDraft(group, alice); d.Step != model.StepText || d.Data.Text != "" {
		t.Fatalf("draft changed: %+v", d)
	}
	if n := len(s.send.Messages()); n != sentBefore {
		t.Errorf("%d messages were sent in answer to ignored texts", n-sentBefore)
	}
	if n := len(s.todos(group)); n != 0 {
		t.Fatalf("%d items from ignored texts", n)
	}
	s.noDraft(group, bob)

	// Another member's press gets a toast and changes nothing.
	toast := s.wpress(bob, group, card, ui.ActWizNav, ui.WizNavCancel)
	if toast != bobLc.T("wizard.toast.foreign") {
		t.Errorf("toast %q, want %q", toast, bobLc.T("wizard.toast.foreign"))
	}
	s.mustDraft(group, alice)

	// The owner answers by replying to the card itself.
	s.wsay(alice, group, "water the plants", card)
	if d := s.mustDraft(group, alice); d.Data.Text != "water the plants" || d.Step != model.StepDate {
		t.Fatalf("draft %+v", d)
	}
	s.wpress(alice, group, card, ui.ActWizDate, dayArg(2026, 10, 11))

	// Typing a time asks a new question; its reply is taken.
	s.wpress(alice, group, card, ui.ActWizTime, ui.WizTimeType)
	asks := s.asks(group)
	if len(asks) != 2 || asks[1].Msg.ReplyTo != card {
		t.Fatalf("questions %+v, want a second one replying to the card", asks)
	}
	s.wsay(alice, group, "3pm", 0) // not a reply: ignored
	if d := s.mustDraft(group, alice); d.Step != model.StepTimeText {
		t.Fatalf("a non-reply moved the draft to %s", d.Step)
	}
	s.wsay(alice, group, "3pm", asks[1].MsgID)
	if d := s.mustDraft(group, alice); d.Step != model.StepRepeat || d.Data.Time != "15:00" {
		t.Fatalf("draft %+v", d)
	}
	s.wpress(alice, group, card, ui.ActWizRepeat, ui.WizRepeatOnce)
	s.wpress(alice, group, card, ui.ActWizAssign, ui.WizAssignNone)
	s.wpress(alice, group, card, ui.ActWizNav, ui.WizNavSave)

	td := s.todoByNum(group, 1)
	if td.Text != "water the plants" || td.AssigneeID != 0 || td.CreatedBy != alice || !td.DueAt.Equal(utc(2026, 10, 11, 15, 0)) {
		t.Errorf("item %+v", td)
	}
}

// ---- Back, Skip, Cancel and Edit through the whole path ----

func TestWizardBackSkipEditCancel(t *testing.T) {
	s := newWizardStack(t)
	lc := s.locale(alice)
	s.say(alice, alice, "/new")
	card := s.card(alice)
	s.say(alice, alice, "report")
	s.wpress(alice, alice, card, ui.ActWizDate, dayArg(2026, 10, 12))
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavBack)
	if d := s.mustDraft(alice, alice); d.Step != model.StepDate {
		t.Fatalf("back from time: %s", d.Step)
	}
	s.wpress(alice, alice, card, ui.ActWizDate, dayArg(2026, 10, 12))
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSkip) // time: default
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSkip) // repeat: once
	d := s.mustDraft(alice, alice)
	if d.Step != model.StepConfirm || d.Data.Repeat != model.RepeatOnce || d.Data.Time != "" {
		t.Fatalf("after skips: %+v", d)
	}

	// Edit the date: it walks date, time, repeat to the summary again.
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavEdit)
	s.wpress(alice, alice, card, ui.ActWizEdit, ui.WizEditDate)
	s.wpress(alice, alice, card, ui.ActWizDate, dayArg(2026, 10, 13))
	s.wpress(alice, alice, card, ui.ActWizTime, "1800")
	s.wpress(alice, alice, card, ui.ActWizRepeat, ui.WizRepeatDaily)
	d = s.mustDraft(alice, alice)
	if d.Step != model.StepConfirm || d.Data.Date != "2026-10-13" || d.Data.Time != "18:00" || d.Data.Repeat != model.RepeatDaily {
		t.Fatalf("after the date edit: %+v", d)
	}

	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavCancel)
	s.noDraft(alice, alice)
	if got := s.shown(alice, card).Msg.Text; got != lc.T("wizard.cancelled") {
		t.Errorf("cancelled card %q", got)
	}
	if n := len(s.todos(alice)); n != 0 {
		t.Errorf("%d items after Cancel", n)
	}
}

// ---- two nodes ----

func TestWizardStartedOnNodeAFinishedOnNodeB(t *testing.T) {
	clk := clocktest.New(wizardStart())
	m := newMockTelegram(t)
	db := dbURL(t.TempDir())
	lc := i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}

	a := startNode(t, m, db, clk, "node-a")
	waitUntil(t, "node A to poll", func() bool { return m.polled(a.token) })
	b := startNode(t, m, db, clk, "node-b")

	var upd int64 = 100
	say := func(text string) {
		upd++
		m.queueMessageFrom(upd, alice, dadName, alice, text, 0)
	}
	press := func(card int, kind ui.ActionKind, arg string) {
		d, ok := m.button(alice, card, kind, arg)
		if !ok {
			t.Fatalf("no %s/%q button on card %d", kind, arg, card)
		}
		upd++
		m.queueCallback(upd, alice, dadName, alice, card, d)
	}
	waitButton := func(card int, kind ui.ActionKind, arg string) {
		t.Helper()
		waitUntil(t, "the "+string(kind)+"/"+arg+" button", func() bool { _, ok := m.button(alice, card, kind, arg); return ok })
	}

	say("/new")
	waitUntil(t, "the wizard card", func() bool { return m.wizardCard(alice) != 0 })
	card := m.wizardCard(alice)
	waitButton(card, ui.ActWizNav, ui.WizNavCancel) // the text step is drawn
	say("call the dentist")
	tmrw := dayArg(2026, 10, 11)
	waitButton(card, ui.ActWizDate, tmrw)
	press(card, ui.ActWizDate, tmrw)
	waitButton(card, ui.ActWizTime, "1500")
	press(card, ui.ActWizTime, "1500")
	waitButton(card, ui.ActWizRepeat, ui.WizRepeatWeekly)
	for _, c := range m.allCalls() {
		if c.Method == "editMessageText" && c.Token != a.token {
			t.Fatalf("node B edited before the failover: %+v", c)
		}
	}

	// Node A stops and releases the lease; B takes over, and is served the
	// updates again, which MarkUpdate drops.
	a.stop()
	clk.Advance(leaseTTL/3 + time.Second)
	waitUntil(t, "node B to take over", func() bool { return m.polled(b.token) })
	editsByA := m.count(func(c tgCall) bool { return c.Method == "editMessageText" && c.Token == a.token })

	press(card, ui.ActWizRepeat, ui.WizRepeatWeekly)
	waitButton(card, ui.ActWizNav, ui.WizNavSave)
	press(card, ui.ActWizNav, ui.WizNavSave)
	waitUntil(t, "the item", func() bool {
		n, err := b.st.CountTodos(context.Background(), model.TodoQuery{ChatID: alice})
		return err == nil && n == 1
	})
	waitUntil(t, "the saved card", func() bool {
		c, ok := m.latestFor(alice, card)
		return ok && len(c.Buttons) == 0 && strings.Contains(c.Text, "call the dentist")
	})

	td := b.todoByNum(alice, 1)
	if td.Text != "call the dentist" || td.Recur != "weekly sun 15:00" || !td.DueAt.Equal(utc(2026, 10, 11, 15, 0)) {
		t.Errorf("item %+v", td)
	}
	if n := m.count(func(c tgCall) bool { return c.Method == "editMessageText" && c.Token == a.token }); n != editsByA {
		t.Errorf("node A edited after it stopped")
	}
	if n := m.count(func(c tgCall) bool { return c.Method == "editMessageText" && c.Token == b.token }); n < 2 {
		t.Errorf("node B made %d edits, want its steps", n)
	}
	b.noDraft(alice, alice)
	// The wizard card was the only message with the placeholder; a replayed
	// "/new" must not have started a second wizard.
	if got := len(m.sent(lc.T("wizard.starting"))); got != 1 {
		t.Errorf("%d wizards were started, want 1", got)
	}
}
