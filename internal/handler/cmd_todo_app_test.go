package handler

import (
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// withWebApp turns the web on for one test. The tests of this package do not
// run in parallel, so the package-level origin is safe to change here.
func withWebApp(t *testing.T, origin string) {
	t.Helper()
	SetWebApp(origin)
	t.Cleanup(func() { SetWebApp("") })
}

func onlyButton(t *testing.T, kb [][]tg.Button) tg.Button {
	t.Helper()
	if len(kb) != 1 || len(kb[0]) != 1 {
		t.Fatalf("keyboard = %+v, want one button", kb)
	}
	return kb[0][0]
}

func TestOpenAppPrivateIsWebAppButton(t *testing.T) {
	for _, cmd := range []string{"/start", "/help"} {
		e := newTodoEnv(t)
		withWebApp(t, "https://todo.example.com/")
		e.say(userEn, userEn, cmd)
		msgs := e.fs.Messages()
		if len(msgs) != 2 {
			t.Fatalf("%s: %d messages, want 2: %+v", cmd, len(msgs), msgs)
		}
		help, app := msgs[0].Msg, msgs[1].Msg
		// The help keeps the "➕ New task" reply keyboard: Telegram takes one
		// markup per message, so the inline button goes in a second message.
		if !strings.Contains(help.Text, "Todo bot") || len(help.ReplyKeyboard) == 0 || help.Keyboard != nil {
			t.Errorf("%s help = %+v", cmd, help)
		}
		b := onlyButton(t, app.Keyboard)
		if b.WebAppURL != "https://todo.example.com/tg/" || b.URL != "" || b.Data != "" {
			t.Errorf("%s button = %+v", cmd, b)
		}
		if b.Text != (i18n.Locale{Lang: i18n.En}).T("todo.app.open") || app.Text != (i18n.Locale{Lang: i18n.En}).T("todo.app.hint") {
			t.Errorf("%s texts = %q, %q", cmd, b.Text, app.Text)
		}
	}
}

func TestOpenAppPrivateFarsi(t *testing.T) {
	e := newTodoEnv(t)
	withWebApp(t, "https://todo.example.com")
	e.say(userFa, userFa, "/start")
	b := onlyButton(t, e.last(t).Msg.Keyboard)
	if b.Text != (i18n.Locale{Lang: i18n.Fa}).T("todo.app.open") || b.WebAppURL != "https://todo.example.com/tg/" {
		t.Errorf("button = %+v", b)
	}
}

func TestOpenAppGroupIsStartAppLink(t *testing.T) {
	for _, cmd := range []string{"/start", "/help"} {
		e := newTodoEnv(t)
		withWebApp(t, "https://todo.example.com")
		e.say(userEn, group, cmd)
		msgs := e.fs.Messages()
		if len(msgs) != 1 {
			t.Fatalf("%s: %d messages, want 1: %+v", cmd, len(msgs), msgs)
		}
		m := msgs[0].Msg
		if !strings.Contains(m.Text, "Todo bot") || len(m.ReplyKeyboard) != 0 || m.ReplyTo == 0 {
			t.Errorf("%s message = %+v", cmd, m)
		}
		// web_app inline buttons work only in private chats.
		b := onlyButton(t, m.Keyboard)
		if b.URL != "https://t.me/TodoBot?startapp=g-500" || b.WebAppURL != "" || b.Data != "" {
			t.Errorf("%s button = %+v", cmd, b)
		}
	}
}

func TestOpenAppGroupWithoutUsername(t *testing.T) {
	e := newTodoEnv(t)
	e.h.BotUsername = ""
	withWebApp(t, "https://todo.example.com")
	e.say(userEn, group, "/help")
	if kb := e.last(t).Msg.Keyboard; kb != nil {
		t.Errorf("keyboard without a bot username = %+v", kb)
	}
}

func TestOpenAppWebOff(t *testing.T) {
	for _, chat := range []int64{userEn, group} {
		for _, cmd := range []string{"/start", "/help"} {
			e := newTodoEnv(t)
			e.say(userEn, chat, cmd)
			msgs := e.fs.Messages()
			if len(msgs) != 1 || msgs[0].Msg.Keyboard != nil {
				t.Errorf("chat %d %s: %+v", chat, cmd, msgs)
			}
		}
	}
}

func TestSetWebAppTrimsSlash(t *testing.T) {
	withWebApp(t, "https://todo.example.com///")
	if got := webAppURL(); got != "https://todo.example.com/tg/" {
		t.Errorf("webAppURL = %q", got)
	}
	SetWebApp("")
	if got := webAppURL(); got != "" {
		t.Errorf("web off: webAppURL = %q", got)
	}
}
