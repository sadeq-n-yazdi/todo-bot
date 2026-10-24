package handler

import (
	"context"
	"errors"
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

const (
	userEn  int64 = 1
	userFa  int64 = 2
	userTeh int64 = 3 // Farsi, Jalali, Asia/Tehran (UTC+3:30)
	userBad int64 = 99
	group   int64 = -500
)

type todoEnv struct {
	h   *Handler
	fs  *tgtest.FakeSender
	svc *fakeSvc11
}

func newTodoEnv(t *testing.T) *todoEnv {
	t.Helper()
	clk := clocktest.New(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	svc := newFakeSvc11(clk.Now)
	svc.prefs[userEn] = model.Prefs{TZ: time.UTC, Locale: i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}}
	svc.prefs[userFa] = model.Prefs{TZ: time.UTC, Locale: i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}}
	tehranLoc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	svc.prefs[userTeh] = model.Prefs{TZ: tehranLoc, Locale: i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}}
	fs := &tgtest.FakeSender{}
	h := New(Deps{
		Svc: svc, Send: fs, Clock: clk,
		Parser:  nlparse.Parser{DefaultHour: 9},
		Allowed: map[int64]bool{userEn: true, userFa: true, userTeh: true}, BotUsername: "TodoBot",
	})
	return &todoEnv{h: h, fs: fs, svc: svc}
}

// say sends text as user in chat (private when chat == user).
func (e *todoEnv) say(user, chat int64, text string) {
	kind := model.ChatPrivate
	if chat < 0 {
		kind = model.ChatGroup
	}
	err := e.h.Handle(context.Background(), tg.Update{Message: &tg.Message{
		ID: 7, ChatID: chat, ChatKind: kind, FromID: user, Text: text}})
	if err != nil {
		panic(err)
	}
}

func (e *todoEnv) last(t *testing.T) tgtest.Sent {
	t.Helper()
	s, ok := e.fs.Last()
	if !ok {
		t.Fatal("no message sent")
	}
	return s
}

func (e *todoEnv) openTexts(chat int64) []string {
	var out []string
	for _, td := range e.svc.open(chat) {
		out = append(out, td.Text)
	}
	return out
}

func TestStrangerIgnored(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userBad, userBad, "/add secret")
	e.say(userBad, userBad, "plain text")
	if len(e.fs.Messages()) != 0 || len(e.svc.todos) != 0 {
		t.Fatalf("stranger was served: %+v", e.fs.Messages())
	}
}

func TestStartHelpLocalised(t *testing.T) {
	for _, cmd := range []string{"/start", "/help"} {
		e := newTodoEnv(t)
		e.say(userEn, userEn, cmd)
		if got := e.last(t).Msg.Text; !strings.Contains(got, "Todo bot") || !strings.Contains(got, "/add") {
			t.Errorf("en %s: %q", cmd, got)
		}
		e.say(userFa, userFa, cmd)
		if got := e.last(t).Msg.Text; !strings.Contains(got, "ربات کارها") || !strings.Contains(got, "/add") {
			t.Errorf("fa %s: %q", cmd, got)
		}
	}
}

func TestAddCommandAndPlainText(t *testing.T) {
	tests := []struct {
		name, text string
		chat       int64
		user       int64
		want       []string // open texts afterwards
	}{
		{"command", "/add buy milk", userEn, userEn, []string{"buy milk"}},
		{"plain private", "buy milk", userEn, userEn, []string{"buy milk"}},
		{"plain in group ignored", "buy milk", group, userEn, nil},
		{"command in group", "/add@TodoBot buy milk", group, userEn, []string{"buy milk"}},
		{"command for other bot ignored", "/add@OtherBot buy milk", group, userEn, nil},
		{"date stripped", "/add buy milk tomorrow 18:00", userEn, userEn, []string{"buy milk"}},
		{"persian digits in text kept", "خرید ۲ نان", userFa, userFa, []string{"خرید ۲ نان"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newTodoEnv(t)
			e.say(tc.user, tc.chat, tc.text)
			got := e.openTexts(tc.chat)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("open = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAddSetsDueAndReplies(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add buy milk tomorrow 18:00")
	td := e.svc.todos[0]
	want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if td.DueAt == nil || !td.DueAt.Equal(want) {
		t.Fatalf("due = %v, want %v", td.DueAt, want)
	}
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Added 1. buy milk") || !strings.Contains(got, "18:00") {
		t.Fatalf("reply %q", got)
	}
}

func TestAddEmptyShowsUsage(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add")
	e.say(userEn, userEn, "/add tomorrow 18:00") // date only, no text
	if len(e.svc.todos) != 0 {
		t.Fatalf("stored %+v", e.svc.todos[0])
	}
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Usage") {
		t.Fatalf("reply %q", got)
	}
}

func TestAddServiceEmptyTextShowsUsage(t *testing.T) {
	e := newTodoEnv(t)
	e.svc.addErr = service.ErrEmptyText
	e.say(userFa, userFa, "/add x")
	want := i18n.Locale{Lang: i18n.Fa}.T("todo.usage.add")
	if got := e.last(t).Msg.Text; got != want {
		t.Fatalf("reply %q, want %q", got, want)
	}
}

func TestAddEscapesHTML(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add <b>x</b> & y")
	if got := e.last(t).Msg.Text; strings.Contains(got, "<b>x") || !strings.Contains(got, "&lt;b&gt;") {
		t.Fatalf("reply not escaped: %q", got)
	}
}

func TestTextCap(t *testing.T) {
	for _, lang := range []struct {
		user int64
		want string
	}{{userEn, "too long"}, {userFa, "خیلی طولانی"}} {
		e := newTodoEnv(t)
		e.say(lang.user, lang.user, "/add "+strings.Repeat("a", 1001))
		if len(e.svc.todos) != 0 {
			t.Fatal("1001-character /add was stored")
		}
		if got := e.last(t).Msg.Text; !strings.Contains(got, lang.want) {
			t.Errorf("reply %q lacks %q", got, lang.want)
		}
	}
}

func TestTextCapBoundaryAndRunes(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add "+strings.Repeat("a", 1000))
	e.say(userFa, userFa, "/add "+strings.Repeat("م", 1000)) // 2000 bytes, 1000 characters
	if len(e.svc.todos) != 2 {
		t.Fatalf("1000-character items rejected, stored %d", len(e.svc.todos))
	}
	e.say(userEn, userEn, "/edit 1 "+strings.Repeat("b", 1001))
	if got := e.svc.todos[0].Text; strings.HasPrefix(got, "b") {
		t.Fatal("1001-character /edit was applied")
	}
	if got := e.last(t).Msg.Text; !strings.Contains(got, "too long") {
		t.Fatalf("reply %q", got)
	}
	e.say(userEn, userEn, "plain "+strings.Repeat("a", 1001))
	if len(e.svc.todos) != 2 {
		t.Fatal("long plain text was stored")
	}
}

func TestListPagingAndScope(t *testing.T) {
	e := newTodoEnv(t)
	for range 12 {
		e.say(userEn, userEn, "/add item")
	}
	e.say(userEn, group, "/add group item")
	e.say(userEn, userEn, "/list")
	first := e.last(t).Msg
	if !strings.Contains(first.Text, "1/2") || strings.Contains(first.Text, "group item") {
		t.Fatalf("page 1: %q", first.Text)
	}
	e.say(userEn, userEn, "/list 2")
	second := e.last(t).Msg
	if !strings.Contains(second.Text, "2/2") || !strings.Contains(second.Text, "11. item") || strings.Contains(second.Text, "10. item") {
		t.Fatalf("page 2: %q", second.Text)
	}
	e.say(userEn, userEn, "/list 9") // past the end shows the last page
	if got := e.last(t).Msg.Text; !strings.Contains(got, "2/2") {
		t.Fatalf("clamp: %q", got)
	}
	e.say(userEn, group, "/list")
	if got := e.last(t).Msg; !strings.Contains(got.Text, "1. group item") || strings.Contains(got.Text, "11.") {
		t.Fatalf("group list: %q", got.Text)
	}
	e.say(userEn, userEn, "/list abc")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Usage") {
		t.Fatalf("bad page: %q", got)
	}
}

func TestListFarsiAndEmpty(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userFa, userFa, "/list")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "کاری نمانده") {
		t.Fatalf("empty: %q", got)
	}
	e.say(userFa, userFa, "/add نان")
	e.say(userFa, userFa, "/list")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "۱. نان") {
		t.Fatalf("persian digits: %q", got)
	}
}

func TestDoneDelEdit(t *testing.T) {
	e := newTodoEnv(t)
	for _, s := range []string{"a", "b", "c"} {
		e.say(userEn, userEn, "/add "+s)
	}
	e.say(userEn, userEn, "/done 1")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Done: 1. a") {
		t.Fatalf("done reply %q", got)
	}
	e.say(userEn, userEn, "/del #2")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Deleted: 2. b") {
		t.Fatalf("del reply %q", got)
	}
	e.say(userEn, userEn, "/edit 3 c2 text")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Updated: 3. c2 text") {
		t.Fatalf("edit reply %q", got)
	}
	if got := strings.Join(e.openTexts(userEn), "|"); got != "c2 text" {
		t.Fatalf("open = %q", got)
	}
}

func TestPersianDigitsInNumbers(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userFa, userFa, "/add نان")
	e.say(userFa, userFa, "/done ۱")
	if len(e.openTexts(userFa)) != 0 {
		t.Fatal("/done ۱ did not complete item 1")
	}
	if got := e.last(t).Msg.Text; !strings.Contains(got, "انجام شد: ۱. نان") {
		t.Fatalf("reply %q", got)
	}
}

func TestBadArgsAndErrors(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add a")
	tests := []struct {
		name, text, want string
	}{
		{"done no arg", "/done", "Usage"},
		{"done word", "/done x", "Usage"},
		{"done zero", "/done 0", "Usage"},
		{"done missing", "/done 5", "no open item"},
		{"del missing", "/del 5", "no open item"},
		{"edit missing", "/edit 5 hi", "no open item"},
		{"edit no args", "/edit", "Usage"},
		{"unknown private", "/frobnicate", "Unknown command"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e.fs.Reset()
			e.say(userEn, userEn, tc.text)
			if got := e.last(t).Msg.Text; !strings.Contains(got, tc.want) {
				t.Fatalf("reply %q lacks %q", got, tc.want)
			}
		})
	}
	if got := strings.Join(e.openTexts(userEn), "|"); got != "a" {
		t.Fatalf("state changed: %q", got)
	}
}

func TestUnknownCommandSilentInGroup(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, group, "/frobnicate")
	if len(e.fs.Messages()) != 0 {
		t.Fatal("replied to unknown command in a group")
	}
}

func TestServiceErrorsMapped(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{service.ErrNotFound, "no open item"},
		{service.ErrForeignChat, "another chat"},
		{service.ErrInPast, "in the past"},
	}
	for _, tc := range tests {
		for _, cmd := range []string{"/done 1", "/del 1", "/edit 1 x"} {
			e := newTodoEnv(t)
			e.say(userEn, userEn, "/add a")
			e.svc.failWith = tc.err
			e.fs.Reset()
			e.say(userEn, userEn, cmd)
			if got := e.last(t).Msg.Text; !strings.Contains(got, tc.want) {
				t.Errorf("%s with %v: reply %q", cmd, tc.err, got)
			}
		}
	}
}

func TestUnexpectedErrorGeneric(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add a")
	e.svc.failWith = errors.New("boom")
	e.say(userEn, userEn, "/done 1")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "Something went wrong") {
		t.Fatalf("reply %q", got)
	}
}

func TestDataScopedPerChat(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add private one")
	e.say(userEn, group, "/add group one")
	e.say(userEn, group, "/done 1")
	if got := strings.Join(e.openTexts(userEn), "|"); got != "private one" {
		t.Fatalf("private = %q", got)
	}
	if len(e.openTexts(group)) != 0 {
		t.Fatal("group item still open")
	}
	// Group replies quote the command.
	if got := e.last(t); got.Msg.ReplyTo != 7 || got.ChatID != group {
		t.Fatalf("group reply %+v", got)
	}
}

func TestToday(t *testing.T) {
	e := newTodoEnv(t)
	e.say(userEn, userEn, "/add call mom today 18:00")
	e.say(userEn, userEn, "/today")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "call mom") {
		t.Fatalf("en today: %q", got)
	}
	e.say(userFa, userFa, "/today")
	if got := e.last(t).Msg.Text; !strings.Contains(got, "امروز کاری ندارید") {
		t.Fatalf("fa today: %q", got)
	}
}

func TestTodoMessagesHaveBothLanguages(t *testing.T) {
	fa := map[string]bool{}
	for _, k := range i18n.Keys(i18n.Fa) {
		fa[k] = true
	}
	n := 0
	for _, k := range i18n.Keys(i18n.En) {
		if !strings.HasPrefix(k, "todo.") {
			continue
		}
		n++
		if !fa[k] {
			t.Errorf("key %s has no Farsi text", k)
		}
	}
	if n == 0 {
		t.Fatal("no todo.* keys registered")
	}
}
