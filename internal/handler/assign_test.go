package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

// fakeAssign is an in-memory service for the assignment tests. Methods it
// does not define panic through the nil embedded interface.
type fakeAssign struct {
	service.Service
	prefs   map[int64]model.Prefs
	users   map[string]model.User // by lowercase username
	todos   []*model.Todo
	rems    map[int64]int64 // reminder id -> todo id
	noted   map[int64]string
	assigns []int64 // assignee ids passed to AddTodoFor
	added   int     // AddTodo calls
}

func (f *fakeAssign) Prefs(_ context.Context, id int64, _ string) (model.Prefs, error) {
	return f.prefs[id], nil
}
func (f *fakeAssign) MarkUpdate(context.Context, int64) (bool, error) { return true, nil }
func (f *fakeAssign) NoteUsername(_ context.Context, id int64, n string) error {
	f.noted[id] = n
	return nil
}
func (f *fakeAssign) UserByUsername(_ context.Context, n string) (model.User, error) {
	if u, ok := f.users[strings.ToLower(n)]; ok {
		return u, nil
	}
	return model.User{}, service.ErrNotFound
}
func (f *fakeAssign) insert(a service.Actor, assignee int64, r nlparse.Result) model.Todo {
	num := 1
	for _, t := range f.todos {
		if t.ChatID == a.ChatID {
			num++
		}
	}
	t := &model.Todo{ID: int64(len(f.todos) + 1), ChatID: a.ChatID, Num: num, Text: r.Text,
		Status: model.TodoOpen, DueAt: r.Due, CreatedBy: a.UserID, AssigneeID: assignee}
	for _, u := range f.users {
		if u.ID == assignee {
			t.AssigneeName = u.Username
		}
	}
	f.todos = append(f.todos, t)
	return *t
}
func (f *fakeAssign) AddTodo(_ context.Context, a service.Actor, r nlparse.Result) (model.Todo, error) {
	f.added++
	return f.insert(a, 0, r), nil
}
func (f *fakeAssign) AddTodoFor(_ context.Context, a service.Actor, as int64, r nlparse.Result) (model.Todo, error) {
	f.assigns = append(f.assigns, as)
	return f.insert(a, as, r), nil
}
func (f *fakeAssign) List(_ context.Context, chat int64, _ int) ([]model.Todo, int, error) {
	var out []model.Todo
	for _, t := range f.todos {
		if t.ChatID == chat && t.Status == model.TodoOpen {
			out = append(out, *t)
		}
	}
	return out, 1, nil
}
func (f *fakeAssign) ListAssigned(_ context.Context, uid int64) ([]model.Todo, error) {
	var out []model.Todo
	for _, t := range f.todos {
		if t.AssigneeID == uid && t.ChatID != uid && t.Status == model.TodoOpen {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (f *fakeAssign) allowed(a service.Actor, id int64) (*model.Todo, error) {
	for _, t := range f.todos {
		if t.ID == id {
			if t.ChatID != a.ChatID && t.AssigneeID != a.UserID {
				return nil, service.ErrForeignChat
			}
			return t, nil
		}
	}
	return nil, service.ErrNotFound
}
func (f *fakeAssign) Complete(_ context.Context, a service.Actor, id int64) (model.Todo, error) {
	t, err := f.allowed(a, id)
	if err != nil {
		return model.Todo{}, err
	}
	t.Status = model.TodoDone
	return *t, nil
}
func (f *fakeAssign) AckReminder(_ context.Context, a service.Actor, rid int64) (model.Todo, error) {
	return f.Complete(context.Background(), a, f.rems[rid])
}

const (
	creator  int64 = 1
	assignee int64 = 2
	other    int64 = 3 // known user, not allowlisted
	grp      int64 = -77
)

type assignEnv struct {
	h   *Handler
	fs  *tgtest.FakeSender
	svc *fakeAssign
}

func newAssignEnv() *assignEnv {
	clk := clocktest.New(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	en := model.Prefs{TZ: time.UTC, Locale: i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}}
	fa := model.Prefs{TZ: time.UTC, Locale: i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}}
	svc := &fakeAssign{
		prefs: map[int64]model.Prefs{creator: en, assignee: fa, other: en},
		users: map[string]model.User{
			"wife":  {ID: assignee, Username: "wife"},
			"ghost": {ID: other, Username: "ghost"},
			"engl":  {ID: creator, Username: "engl"},
		},
		noted: map[int64]string{}, rems: map[int64]int64{},
	}
	fs := &tgtest.FakeSender{}
	h := New(Deps{Svc: svc, Send: fs, Clock: clk, Parser: nlparse.Parser{DefaultHour: 9},
		Allowed: map[int64]bool{creator: true, assignee: true}, BotUsername: "TodoBot"})
	return &assignEnv{h: h, fs: fs, svc: svc}
}

func (e *assignEnv) msg(user, chat int64, text string) {
	kind := model.ChatPrivate
	if chat < 0 {
		kind = model.ChatGroup
	}
	_ = e.h.Handle(context.Background(), tg.Update{Message: &tg.Message{
		ID: 5, ChatID: chat, ChatKind: kind, FromID: user, FromUsername: "u" + text[:1], Text: text}})
}

func (e *assignEnv) press(user, chat int64, data string) {
	_ = e.h.Handle(context.Background(), tg.Update{Callback: &tg.Callback{
		ID: "cb", FromID: user, ChatID: chat, MessageID: 9, Data: data}})
}

func TestSplitMention(t *testing.T) {
	cases := []struct{ in, name, rest string }{
		{"@wife buy milk", "wife", "buy milk"},
		{"@wife: buy milk", "wife", "buy milk"},
		{"@WiFe  buy milk", "WiFe", "buy milk"},
		{"@wife", "", "@wife"},
		{"@abc buy milk", "", "@abc buy milk"},
		{"buy milk @wife", "", "buy milk @wife"},
		{"@todobot buy milk", "", "buy milk"},
		{"@TodoBot", "", "@TodoBot"},
	}
	for _, c := range cases {
		n, r := splitMention(c.in, "TodoBot")
		if n != c.name || r != c.rest {
			t.Errorf("splitMention(%q) = %q,%q want %q,%q", c.in, n, r, c.name, c.rest)
		}
	}
	if n, _ := splitMention("@wi۱۲e buy", "TodoBot"); n != "wi12e" {
		t.Errorf("persian digits in name not normalised: %q", n)
	}
}

func TestAddAssignsAndConfirms(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @wife pick up son today 15:00")
	if len(e.svc.assigns) != 1 || e.svc.assigns[0] != assignee || e.svc.added != 0 {
		t.Fatalf("assigns=%v added=%d", e.svc.assigns, e.svc.added)
	}
	if got := e.svc.todos[0].Text; got != "pick up son" {
		t.Errorf("mention leaked into text: %q", got)
	}
	last, _ := e.fs.Last()
	if !strings.Contains(last.Msg.Text, `<a href="tg://user?id=2">@wife</a>`) || !strings.Contains(last.Msg.Text, "→") {
		t.Errorf("confirmation = %q", last.Msg.Text)
	}
}

func TestPlainPrivateTextAssigns(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, creator, "@wife call mum")
	if len(e.svc.assigns) != 1 {
		t.Fatalf("plain private text with mention did not assign: %+v", e.svc.todos)
	}
}

func TestAddUnknownOrNotAllowed(t *testing.T) {
	for _, name := range []string{"nobody1", "ghost"} {
		e := newAssignEnv()
		e.msg(creator, grp, "/add @"+name+" buy milk")
		if len(e.svc.todos) != 0 {
			t.Fatalf("%s: item created", name)
		}
		last, _ := e.fs.Last()
		want := "@" + name + " hasn't started the bot yet — ask them to open @TodoBot and press Start."
		if last.Msg.Text != want {
			t.Errorf("got %q want %q", last.Msg.Text, want)
		}
	}
}

func TestAddUnknownFarsi(t *testing.T) {
	e := newAssignEnv()
	e.msg(assignee, assignee, "/add @nobody1 x")
	last, _ := e.fs.Last()
	if !strings.Contains(last.Msg.Text, "هنوز ربات را شروع نکرده") || !strings.Contains(last.Msg.Text, "@TodoBot") {
		t.Errorf("got %q", last.Msg.Text)
	}
}

func TestAddBotMentionAndMidTextAreNotAssignments(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @TodoBot buy milk")
	e.msg(creator, grp, "/add tell @wife later")
	if len(e.svc.assigns) != 0 || e.svc.added != 2 {
		t.Fatalf("assigns=%v added=%d", e.svc.assigns, e.svc.added)
	}
	if e.svc.todos[0].Text != "buy milk" || e.svc.todos[1].Text != "tell @wife later" {
		t.Errorf("texts: %q, %q", e.svc.todos[0].Text, e.svc.todos[1].Text)
	}
}

func TestNoteUsernameOnEveryUpdate(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, creator, "/list")
	e.press(assignee, assignee, "cx:0")
	if e.svc.noted[creator] != "u/" {
		t.Errorf("message did not note username: %v", e.svc.noted)
	}
	if _, ok := e.svc.noted[assignee]; !ok {
		t.Error("callback did not note username")
	}
}

func TestListShowsArrowAndAssignedSection(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @wife pick up son")
	e.msg(creator, grp, "/list")
	last, _ := e.fs.Last()
	if !strings.Contains(last.Msg.Text, "→ <a href=\"tg://user?id=2\">@wife</a>") {
		t.Errorf("group list lacks arrow: %q", last.Msg.Text)
	}
	if strings.Contains(last.Msg.Text, "Assigned to me") {
		t.Error("group list must not show the assigned section")
	}
	e.msg(assignee, assignee, "/list")
	last, _ = e.fs.Last()
	if !strings.Contains(last.Msg.Text, "محول\u200cشده به من") || !strings.Contains(last.Msg.Text, "pick up son") {
		t.Errorf("assignee list = %q", last.Msg.Text)
	}
}

func TestAssigneeDoneButtonPostsDidNotice(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @wife pick up son")
	e.fs.Sent = nil
	e.press(assignee, assignee, "td:1")
	if e.svc.todos[0].Status != model.TodoDone {
		t.Fatal("not completed")
	}
	var notice *tgtest.Sent
	for _, s := range e.fs.Messages() {
		if s.ChatID == grp {
			notice = &s
		}
	}
	want := `✅ <a href="tg://user?id=2">@wife</a> did: pick up son`
	if notice == nil || notice.Msg.Text != want {
		t.Fatalf("notice = %+v", notice)
	}
}

func TestDidNoticeUsesCreatorLanguage(t *testing.T) {
	e := newAssignEnv()
	// A Farsi user assigns to an English one; the English user completes it
	// in their DM, and the notice to the group is in Farsi.
	e.msg(assignee, grp, "/add @engl ping")
	e.fs.Sent = nil
	e.press(creator, creator, "td:1")
	var notice *tgtest.Sent
	for _, s := range e.fs.Messages() {
		if s.ChatID == grp {
			notice = &s
		}
	}
	if notice == nil || !strings.Contains(notice.Msg.Text, "انجام داد: ping") {
		t.Errorf("got %+v", notice)
	}
}

func TestNoNoticeWhenCreatorOrSameChat(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @wife a")
	e.msg(creator, grp, "/add @wife b")
	e.fs.Sent = nil
	e.press(creator, grp, "td:1")  // creator completes in the group
	e.press(assignee, grp, "td:2") // assignee completes inside the group
	for _, s := range e.fs.Messages() {
		if !s.Edit {
			t.Errorf("unexpected notice: %+v", s)
		}
	}
}

func TestReminderDoneInDMPostsNotice(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @wife pick up son")
	e.svc.rems[40] = 1
	e.fs.Sent = nil
	e.press(assignee, assignee, "rd:40")
	var inGroup, edits int
	for _, s := range e.fs.Messages() {
		if s.ChatID == grp && !s.Edit {
			inGroup++
		}
		if s.Edit && s.ChatID == assignee && s.MsgID == 9 {
			edits++
		}
	}
	if inGroup != 1 || edits != 1 {
		t.Errorf("notices=%d edits(in DM, by callback msg)=%d: %+v", inGroup, edits, e.fs.Messages())
	}
}

func TestAssignedSectionCappedWithMore(t *testing.T) {
	e := newAssignEnv()
	for range 25 {
		e.msg(creator, grp, "/add @wife task")
	}
	e.msg(assignee, assignee, "/list")
	last, _ := e.fs.Last()
	if n := strings.Count(last.Msg.Text, "• task"); n != 20 {
		t.Errorf("shown %d, want 20", n)
	}
	if !strings.Contains(last.Msg.Text, "…و ۵ مورد دیگر") {
		t.Errorf("no translated 'more' line: %q", last.Msg.Text)
	}
	if n := len(last.Msg.Keyboard); n != 20 {
		t.Errorf("rows %d", n)
	}
}

func TestDidNoticeSendFailureIsNotFatal(t *testing.T) {
	e := newAssignEnv()
	e.msg(creator, grp, "/add @wife x")
	e.fs.ErrFn = func(chat int64, _ tg.OutMessage) error {
		if chat == grp {
			return context.DeadlineExceeded
		}
		return nil
	}
	e.press(assignee, assignee, "td:1")
	if e.svc.todos[0].Status != model.TodoDone {
		t.Fatal("completion lost")
	}
}
