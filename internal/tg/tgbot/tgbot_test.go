package tgbot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// call is one recorded Bot API request.
type call struct {
	Method string
	Form   url.Values
}

// fakeAPI is an httptest stand-in for the Telegram Bot API.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	calls   []call
	replies map[string]string // method -> raw JSON body; default {"ok":true,"result":true}
	updates []string          // raw update JSON queued for getUpdates
	wake    chan struct{}     // signalled (cap 1) by queue to release a waiting getUpdates
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, replies: map[string]string{}, wake: make(chan struct{}, 1)}
	f.replies["getMe"] = `{"ok":true,"result":{"id":42,"is_bot":true,"first_name":"Todo","username":"TodoBot"}}`
	f.replies["sendMessage"] = `{"ok":true,"result":{"message_id":77,"date":1,"chat":{"id":5,"type":"private"}}}`
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/bottok/") {
		http.NotFound(w, r)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/bottok/")
	_ = r.ParseMultipartForm(1 << 20)
	f.mu.Lock()
	f.calls = append(f.calls, call{Method: method, Form: r.Form})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if method == "getUpdates" {
		// Emulate a real long poll: block until updates are queued (or the
		// client goes away) instead of answering empty at once. Otherwise a
		// getUpdates that arrives just before queue() would hang and lose them.
		var batch []string
		timeout := time.After(30 * time.Second) // outlives the test
		for len(batch) == 0 {
			f.mu.Lock()
			batch = f.updates
			f.updates = nil
			f.mu.Unlock()
			if len(batch) > 0 {
				break
			}
			select {
			case <-f.wake:
			case <-r.Context().Done():
				return
			case <-timeout:
				return
			}
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[` + strings.Join(batch, ",") + `]}`))
		return
	}
	f.mu.Lock()
	body, ok := f.replies[method]
	f.mu.Unlock()
	if !ok {
		body = `{"ok":true,"result":true}`
	}
	_, _ = w.Write([]byte(body))
}

func (f *fakeAPI) reply(method, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[method] = body
}

func (f *fakeAPI) queue(upd ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, upd...)
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (f *fakeAPI) callsTo(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeAPI) newBot(t *testing.T) *Bot {
	t.Helper()
	b, err := New(context.Background(), "tok", Options{ServerURL: f.srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestNewCallsGetMe(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	if b.Username() != "TodoBot" {
		t.Fatalf("Username = %q", b.Username())
	}
	if n := len(f.callsTo("getMe")); n != 1 {
		t.Fatalf("getMe calls = %d, want 1", n)
	}
}

func TestNewFailsOnBadToken(t *testing.T) {
	f := newFakeAPI(t)
	f.reply("getMe", `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	if _, err := New(context.Background(), "tok", Options{ServerURL: f.srv.URL}); err == nil {
		t.Fatal("want error")
	}
}

const (
	privateMsg = `{"update_id":100,"message":{"message_id":9,"date":1,"text":"/add@TodoBot buy milk",
		"chat":{"id":5,"type":"private"},
		"from":{"id":5,"is_bot":false,"first_name":"Ali","last_name":"Rezai","username":"AliR","language_code":"fa"},
		"reply_to_message":{"message_id":8,"date":1,"chat":{"id":5,"type":"private"}}}}`
	groupMsg = `{"update_id":101,"message":{"message_id":10,"date":1,"text":"/list@OtherBot",
		"chat":{"id":-100,"type":"supergroup","title":"Team"},
		"from":{"id":6,"is_bot":false,"first_name":"Sara","language_code":"en-GB"}}}`
	migrateMsg = `{"update_id":102,"message":{"message_id":11,"date":1,
		"chat":{"id":-100,"type":"group","title":"Team"},
		"from":{"id":6,"is_bot":false,"first_name":"Sara"},
		"migrate_to_chat_id":-1001234}}`
	callbackUpd = `{"update_id":103,"callback_query":{"id":"cb1","chat_instance":"x","data":"done:3",
		"from":{"id":5,"is_bot":false,"first_name":"Ali","username":"AliR","language_code":"fa"},
		"message":{"message_id":9,"date":1,"chat":{"id":5,"type":"private"}}}}`
	ignoredUpd = `{"update_id":104,"edited_message":{"message_id":9,"date":1,"chat":{"id":5,"type":"private"}}}`
)

// collect runs the poller until want updates arrived, then cancels it.
func collect(t *testing.T, b *Bot, want int, h func(tg.Update) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.Poller().Run(ctx, func(_ context.Context, u tg.Update) error {
			err := h(u)
			n++
			if n == want {
				cancel()
			}
			return err
		})
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second): // hang guard only; delivery is event-driven
		t.Fatal("poller did not deliver the expected updates")
	}
}

func TestPollerConvertsUpdates(t *testing.T) {
	f := newFakeAPI(t)
	f.queue(privateMsg, groupMsg, migrateMsg, callbackUpd, ignoredUpd)
	b := f.newBot(t)

	var got []tg.Update
	collect(t, b, 4, func(u tg.Update) error { got = append(got, u); return nil })

	if len(got) != 4 {
		t.Fatalf("got %d updates, want 4 (edited_message must be skipped)", len(got))
	}

	p := got[0]
	if p.ID != 100 || p.Message == nil || p.Callback != nil {
		t.Fatalf("private update = %+v", p)
	}
	wantP := tg.Message{
		ID: 9, ChatID: 5, ChatKind: model.ChatPrivate, FromID: 5,
		FromName: "Ali Rezai", FromUsername: "AliR", FromLang: "fa", Text: "/add buy milk", ReplyToID: 8,
	}
	if *p.Message != wantP {
		t.Errorf("private message = %+v\nwant %+v", *p.Message, wantP)
	}

	g := got[1]
	wantG := tg.Message{
		ID: 10, ChatID: -100, ChatKind: model.ChatGroup, ChatTitle: "Team", FromID: 6,
		FromName: "Sara", FromLang: "en-GB", Text: "/list@OtherBot",
	}
	if g.ID != 101 || *g.Message != wantG {
		t.Errorf("group update = %+v msg %+v\nwant %+v", g, *g.Message, wantG)
	}

	m := got[2]
	if m.ID != 102 || m.Message.MigrateToChatID != -1001234 || m.Message.ChatKind != model.ChatGroup {
		t.Errorf("migrate update = %+v msg %+v", m, *m.Message)
	}

	c := got[3]
	wantC := tg.Callback{ID: "cb1", FromID: 5, FromUsername: "AliR", FromLang: "fa", ChatID: 5, MessageID: 9, Data: "done:3"}
	if c.ID != 103 || c.Message != nil || c.Callback == nil || *c.Callback != wantC {
		t.Errorf("callback update = %+v cb %+v\nwant %+v", c, c.Callback, wantC)
	}
}

func TestPollerAdvancesOffsetAndSurvivesHandlerError(t *testing.T) {
	f := newFakeAPI(t)
	f.queue(privateMsg, groupMsg)
	b := f.newBot(t)

	calls := 0
	collect(t, b, 3, func(tg.Update) error {
		calls++
		if calls == 2 {
			f.queue(migrateMsg) // delivered by the next getUpdates
		}
		return errors.New("dedupe store down")
	})
	if calls != 3 {
		t.Fatalf("handler calls = %d, want 3 (a handler error must not stop the poller)", calls)
	}
	cs := f.callsTo("getUpdates")
	if len(cs) < 2 {
		t.Fatalf("getUpdates calls = %d", len(cs))
	}
	if off := cs[1].Form.Get("offset"); off != "102" {
		t.Fatalf("second offset = %q, want 102 (last update_id + 1)", off)
	}
}

func TestPollerStopsWithin1s(t *testing.T) {
	f := newFakeAPI(t) // getUpdates hangs: nothing queued
	b := f.newBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Poller().Run(ctx, func(context.Context, tg.Update) error { return nil }) }()

	deadline := time.Now().Add(2 * time.Second)
	for len(f.callsTo("getUpdates")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("poller never called getUpdates")
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	select {
	case <-done:
		if d := time.Since(start); d > time.Second {
			t.Fatalf("shutdown took %v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("poller still running 1s after cancel")
	}
}

func TestSend(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)

	id, err := b.Send(context.Background(), 5, tg.OutMessage{
		Text:    "<b>hi</b>",
		ReplyTo: 8,
		Keyboard: [][]tg.Button{
			{{Text: "Done", Data: "done:3"}, {Text: "Del", Data: "del:3"}},
			{{Text: "More", Data: "more"}},
		},
	})
	if err != nil || id != 77 {
		t.Fatalf("Send = %d, %v", id, err)
	}
	c := f.callsTo("sendMessage")[0].Form
	if c.Get("chat_id") != "5" || c.Get("text") != "<b>hi</b>" || c.Get("parse_mode") != "HTML" {
		t.Errorf("form = %v", c)
	}
	var rp struct {
		MessageID int `json:"message_id"`
	}
	if err := json.Unmarshal([]byte(c.Get("reply_parameters")), &rp); err != nil || rp.MessageID != 8 {
		t.Errorf("reply_parameters = %q (%v)", c.Get("reply_parameters"), err)
	}
	var kb struct {
		Rows [][]struct {
			Text string `json:"text"`
			Data string `json:"callback_data"`
		} `json:"inline_keyboard"`
	}
	if err := json.Unmarshal([]byte(c.Get("reply_markup")), &kb); err != nil {
		t.Fatalf("reply_markup %q: %v", c.Get("reply_markup"), err)
	}
	if len(kb.Rows) != 2 || len(kb.Rows[0]) != 2 || kb.Rows[0][1].Data != "del:3" || kb.Rows[1][0].Text != "More" {
		t.Errorf("keyboard = %+v", kb)
	}
}

func TestSendForceReplyAndPlain(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)

	if _, err := b.Send(context.Background(), 5, tg.OutMessage{Text: "title?", ForceReply: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Send(context.Background(), 5, tg.OutMessage{Text: "plain"}); err != nil {
		t.Fatal(err)
	}
	cs := f.callsTo("sendMessage")
	if got := cs[0].Form.Get("reply_markup"); !strings.Contains(got, `"force_reply":true`) {
		t.Errorf("force reply markup = %q", got)
	}
	if cs[1].Form.Get("reply_markup") != "" || cs[1].Form.Get("reply_parameters") != "" {
		t.Errorf("plain message carries markup: %v", cs[1].Form)
	}
}

func TestSendReplyKeyboard(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	ctx := context.Background()
	for _, m := range []tg.OutMessage{
		{Text: "hi", ReplyKeyboard: [][]string{{"➕ New task"}}},
		{Text: "bye", RemoveKeyboard: true},
		// An inline keyboard wins: only one reply_markup is allowed.
		{Text: "both", Keyboard: [][]tg.Button{{{Text: "Ok", Data: "ok"}}}, ReplyKeyboard: [][]string{{"x"}}},
	} {
		if _, err := b.Send(ctx, 5, m); err != nil {
			t.Fatal(err)
		}
	}
	cs := f.callsTo("sendMessage")
	var kb struct {
		Keyboard [][]struct {
			Text string `json:"text"`
		} `json:"keyboard"`
		IsPersistent   bool `json:"is_persistent"`
		ResizeKeyboard bool `json:"resize_keyboard"`
	}
	if err := json.Unmarshal([]byte(cs[0].Form.Get("reply_markup")), &kb); err != nil {
		t.Fatal(err)
	}
	if len(kb.Keyboard) != 1 || kb.Keyboard[0][0].Text != "➕ New task" || !kb.IsPersistent || !kb.ResizeKeyboard {
		t.Errorf("reply keyboard = %q", cs[0].Form.Get("reply_markup"))
	}
	if got := cs[1].Form.Get("reply_markup"); !strings.Contains(got, `"remove_keyboard":true`) {
		t.Errorf("remove markup = %q", got)
	}
	if got := cs[2].Form.Get("reply_markup"); !strings.Contains(got, `"callback_data":"ok"`) || strings.Contains(got, `"keyboard"`) {
		t.Errorf("precedence: markup = %q", got)
	}
}

func TestEditIgnoresReplyKeyboard(t *testing.T) {
	f := newFakeAPI(t)
	f.reply("editMessageText", `{"ok":true,"result":{"message_id":9,"date":1,"chat":{"id":5,"type":"private"}}}`)
	b := f.newBot(t)
	if err := b.Edit(context.Background(), 5, 9, tg.OutMessage{Text: "x", ReplyKeyboard: [][]string{{"y"}}, RemoveKeyboard: true}); err != nil {
		t.Fatal(err)
	}
	if got := f.callsTo("editMessageText")[0].Form.Get("reply_markup"); got != "" {
		t.Errorf("edit carries markup %q", got)
	}
}

func TestEdit(t *testing.T) {
	f := newFakeAPI(t)
	f.reply("editMessageText", `{"ok":true,"result":{"message_id":9,"date":1,"chat":{"id":5,"type":"private"}}}`)
	b := f.newBot(t)

	err := b.Edit(context.Background(), 5, 9, tg.OutMessage{
		Text:     "<i>x</i>",
		Keyboard: [][]tg.Button{{{Text: "Ok", Data: "ok"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := f.callsTo("editMessageText")[0].Form
	if c.Get("chat_id") != "5" || c.Get("message_id") != "9" || c.Get("text") != "<i>x</i>" || c.Get("parse_mode") != "HTML" {
		t.Errorf("form = %v", c)
	}
	if !strings.Contains(c.Get("reply_markup"), `"callback_data":"ok"`) {
		t.Errorf("reply_markup = %q", c.Get("reply_markup"))
	}
}

func TestAnswerCallback(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	if err := b.AnswerCallback(context.Background(), "cb1", "done!"); err != nil {
		t.Fatal(err)
	}
	c := f.callsTo("answerCallbackQuery")[0].Form
	if c.Get("callback_query_id") != "cb1" || c.Get("text") != "done!" {
		t.Errorf("form = %v", c)
	}
}

func TestDeleteWebhook(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	if err := b.DeleteWebhook(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.callsTo("deleteWebhook")) != 1 {
		t.Fatal("deleteWebhook not called")
	}
}

func TestSetCommands(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	cmds := []Command{{Name: "add", Description: "Add a todo"}, {Name: "list", Description: "List todos"}}
	if err := b.SetCommands(context.Background(), cmds, "fa"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetCommands(context.Background(), cmds, ""); err != nil {
		t.Fatal(err)
	}
	cs := f.callsTo("setMyCommands")
	if len(cs) != 2 {
		t.Fatalf("calls = %d, want 2", len(cs))
	}
	if cs[0].Form.Get("language_code") != "fa" || cs[1].Form.Get("language_code") != "" {
		t.Errorf("language codes = %q, %q", cs[0].Form.Get("language_code"), cs[1].Form.Get("language_code"))
	}
	var got []struct{ Command, Description string }
	if err := json.Unmarshal([]byte(cs[0].Form.Get("commands")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Command != "add" || got[1].Description != "List todos" {
		t.Errorf("commands = %+v", got)
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		code      int
		descPart  string
		retry     time.Duration
		migrate   int64
		permanent bool
	}{
		{
			name: "rate limit", code: 429, retry: 7 * time.Second, descPart: "retry after 7",
			body: `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`,
		},
		{
			name: "migrated", code: 400, migrate: -1001234, descPart: "upgraded to a supergroup",
			body: `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-1001234}}`,
		},
		{
			name: "blocked", code: 403, descPart: "bot was blocked by the user", permanent: true,
			body: `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`,
		},
		{
			name: "chat not found", code: 400, descPart: "chat not found", permanent: true,
			body: `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`,
		},
		{
			name: "server error", code: 502, descPart: "Bad Gateway",
			body: `{"ok":false,"error_code":502,"description":"Bad Gateway"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAPI(t)
			b := f.newBot(t)
			f.reply("sendMessage", tc.body)
			f.reply("editMessageText", tc.body)
			f.reply("answerCallbackQuery", tc.body)

			_, sendErr := b.Send(context.Background(), 5, tg.OutMessage{Text: "x"})
			errs := map[string]error{
				"Send": sendErr,
				"Edit": b.Edit(context.Background(), 5, 1, tg.OutMessage{Text: "x"}),
				"Ans":  b.AnswerCallback(context.Background(), "c", "x"),
			}
			for name, err := range errs {
				var ae *tg.APIError
				if !errors.As(err, &ae) {
					t.Fatalf("%s: err = %v, want *tg.APIError", name, err)
				}
				if ae.Code != tc.code || ae.RetryAfter != tc.retry || ae.MigrateTo != tc.migrate {
					t.Errorf("%s: %+v", name, *ae)
				}
				if !strings.Contains(ae.Desc, tc.descPart) {
					t.Errorf("%s: Desc = %q, want it to contain %q", name, ae.Desc, tc.descPart)
				}
				if tg.IsPermanent(err) != tc.permanent {
					t.Errorf("%s: IsPermanent = %v", name, !tc.permanent)
				}
			}
		})
	}
}

func TestContextCancelIsNotAPIError(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Send(ctx, 5, tg.OutMessage{Text: "x"})
	var ae *tg.APIError
	if err == nil || errors.As(err, &ae) {
		t.Fatalf("err = %v, want non-API error", err)
	}
}
