package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// mockTelegram is an httptest Bot API. It serves queued updates to getUpdates
// (every update with update_id >= offset, like Telegram before the update is
// confirmed) and records every call by bot token.
type mockTelegram struct {
	srv *httptest.Server

	mu      sync.Mutex
	updates []json.RawMessage // in update_id order
	ids     []int64
	calls   []tgCall
	nextMsg int

	// onSend, if set, runs inside sendMessage before it answers.
	onSend func(c tgCall)

	// blocked lists chats for which sendMessage answers 403 "bot was blocked
	// by the user", like Telegram does for a user who blocked the bot.
	blocked map[int64]bool

	// members holds the getChatMember status of each pair of chat and user.
	// A pair that is not in the map answers "left".
	members map[[2]int64]string
}

// tgCall is one recorded Bot API call.
type tgCall struct {
	Token, Method string
	ChatID        int64
	UserID        int64 // user_id of getChatMember
	Text          string
	Offset        int64

	// MsgID is the message_id of an edit, or the ID the mock returned for a
	// send (0 for a send that failed).
	MsgID int
	// ReplyTo is the message a send replies to (reply_parameters).
	ReplyTo int
	// Buttons is the callback data of the inline keyboard, row by row.
	Buttons [][]string
	// ReplyKb is the label rows of a reply keyboard.
	ReplyKb [][]string
	// ForceReply and RemoveKb mirror the reply_markup of the same names.
	ForceReply, RemoveKb bool
	// CallbackID belongs to answerCallbackQuery (Text is the toast).
	CallbackID string
}

// replyMarkup is the part of Telegram's reply_markup the mock records.
type replyMarkup struct {
	Inline [][]struct {
		Data string `json:"callback_data"`
	} `json:"inline_keyboard"`
	Keyboard [][]struct {
		Text string `json:"text"`
	} `json:"keyboard"`
	ForceReply     bool `json:"force_reply"`
	RemoveKeyboard bool `json:"remove_keyboard"`
}

func (c *tgCall) readMarkup(form func(string) string) {
	c.MsgID, _ = strconv.Atoi(form("message_id"))
	c.CallbackID = form("callback_query_id")
	var rp struct {
		MessageID int `json:"message_id"`
	}
	if json.Unmarshal([]byte(form("reply_parameters")), &rp) == nil {
		c.ReplyTo = rp.MessageID
	}
	var mk replyMarkup
	if json.Unmarshal([]byte(form("reply_markup")), &mk) != nil {
		return
	}
	for _, row := range mk.Inline {
		var r []string
		for _, b := range row {
			r = append(r, b.Data)
		}
		c.Buttons = append(c.Buttons, r)
	}
	for _, row := range mk.Keyboard {
		var r []string
		for _, b := range row {
			r = append(r, b.Text)
		}
		c.ReplyKb = append(c.ReplyKb, r)
	}
	c.ForceReply, c.RemoveKb = mk.ForceReply, mk.RemoveKeyboard
}

func newMockTelegram(t *testing.T) *mockTelegram {
	m := &mockTelegram{nextMsg: 1000}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockTelegram) serve(w http.ResponseWriter, r *http.Request) {
	token, method, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
	_ = r.ParseMultipartForm(1 << 20)
	c := tgCall{Token: token, Method: method, Text: r.Form.Get("text")}
	c.ChatID, _ = strconv.ParseInt(r.Form.Get("chat_id"), 10, 64)
	c.Offset, _ = strconv.ParseInt(r.Form.Get("offset"), 10, 64)
	c.UserID, _ = strconv.ParseInt(r.Form.Get("user_id"), 10, 64)
	c.readMarkup(r.Form.Get)
	m.mu.Lock()
	idx := len(m.calls)
	m.calls = append(m.calls, c)
	onSend := m.onSend
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "getMe":
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":42,"is_bot":true,"first_name":"T","username":"` + botName + `"}}`))
	case "getUpdates":
		deadline := time.After(150 * time.Millisecond) // a short long-poll
		for {
			if batch := m.pending(c.Offset); len(batch) > 0 {
				_, _ = w.Write([]byte(`{"ok":true,"result":[` + strings.Join(batch, ",") + `]}`))
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-deadline:
				_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	case "sendMessage":
		if onSend != nil {
			onSend(c)
		}
		m.mu.Lock()
		isBlocked := m.blocked[c.ChatID]
		m.mu.Unlock()
		if isBlocked {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`))
			return
		}
		m.mu.Lock()
		m.nextMsg++
		id := m.nextMsg
		m.calls[idx].MsgID = id
		m.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":` + strconv.Itoa(id) +
			`,"date":1,"chat":{"id":` + strconv.FormatInt(c.ChatID, 10) + `,"type":"private"}}}`))
	case "getChatMember":
		m.mu.Lock()
		status, ok := m.members[[2]int64{c.ChatID, c.UserID}]
		m.mu.Unlock()
		if !ok {
			status = "left"
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"` + status + `","user":{"id":` +
			strconv.FormatInt(c.UserID, 10) + `,"is_bot":false,"first_name":"U"}}}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}
}

// setMember makes getChatMember for user in chat answer status, for example
// "member" or "left".
func (m *mockTelegram) setMember(chat, user int64, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members == nil {
		m.members = map[[2]int64]string{}
	}
	m.members[[2]int64{chat, user}] = status
}

// blockChat makes sendMessage to chat fail with 403.
func (m *mockTelegram) blockChat(chat int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blocked == nil {
		m.blocked = map[int64]bool{}
	}
	m.blocked[chat] = true
}

func (m *mockTelegram) pending(offset int64) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for i, id := range m.ids {
		if id >= offset {
			out = append(out, string(m.updates[i]))
		}
	}
	return out
}

// queueMessage makes a private text message from user available to pollers
// under the given update id.
func (m *mockTelegram) queueMessage(updateID, user int64, text string) {
	m.queueMessageFrom(updateID, user, "", user, text, 0)
}

func chatKind(chat int64) string {
	if chat < 0 {
		return "group"
	}
	return "private"
}

// queueMessageFrom queues a text message from user (with a @username, if not
// empty) in chat (a group when negative), as a reply to replyTo if not 0.
func (m *mockTelegram) queueMessageFrom(updateID, user int64, username string, chat int64, text string, replyTo int) {
	msg := map[string]any{
		"message_id": updateID,
		"date":       1,
		"chat":       map[string]any{"id": chat, "type": chatKind(chat), "title": "Team"},
		"from":       map[string]any{"id": user, "is_bot": false, "first_name": "U", "username": username},
		"text":       text,
	}
	if replyTo != 0 {
		msg["reply_to_message"] = map[string]any{
			"message_id": replyTo, "date": 1,
			"chat": map[string]any{"id": chat, "type": chatKind(chat)},
		}
	}
	m.queue(updateID, map[string]any{"update_id": updateID, "message": msg})
}

// queueCallback queues a press of an inline button on message msgID.
func (m *mockTelegram) queueCallback(updateID, user int64, username string, chat int64, msgID int, data string) {
	m.queue(updateID, map[string]any{"update_id": updateID, "callback_query": map[string]any{
		"id":   "cb-" + strconv.FormatInt(updateID, 10),
		"from": map[string]any{"id": user, "is_bot": false, "first_name": "U", "username": username},
		"message": map[string]any{
			"message_id": msgID, "date": 1,
			"chat": map[string]any{"id": chat, "type": chatKind(chat)},
		},
		"data": data,
	}})
}

func (m *mockTelegram) queue(updateID int64, update map[string]any) {
	b, _ := json.Marshal(update)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, b)
	m.ids = append(m.ids, updateID)
}

// allCalls returns a copy of the recorded calls.
func (m *mockTelegram) allCalls() []tgCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]tgCall(nil), m.calls...)
}

// latestFor returns the last send or edit that shows message msgID in chat.
func (m *mockTelegram) latestFor(chat int64, msgID int) (tgCall, bool) {
	calls := m.allCalls()
	for i := len(calls) - 1; i >= 0; i-- {
		c := calls[i]
		if (c.Method == "sendMessage" || c.Method == "editMessageText") && c.ChatID == chat && c.MsgID == msgID {
			return c, true
		}
	}
	return tgCall{}, false
}

// button returns the callback data of the button of message msgID (as it
// shows now) whose action is kind with arg.
func (m *mockTelegram) button(chat int64, msgID int, kind ui.ActionKind, arg string) (string, bool) {
	c, ok := m.latestFor(chat, msgID)
	if !ok {
		return "", false
	}
	for _, row := range c.Buttons {
		for _, d := range row {
			if a, err := ui.Decode(d); err == nil && a.Kind == kind && a.Arg == arg {
				return d, true
			}
		}
	}
	return "", false
}

// wizardCard returns the ID of the newest wizard card sent to chat: a send
// whose keyboard is the wizard's placeholder button. 0 if there is none.
func (m *mockTelegram) wizardCard(chat int64) int {
	calls := m.allCalls()
	for i := len(calls) - 1; i >= 0; i-- {
		c := calls[i]
		if c.Method != "sendMessage" || c.ChatID != chat || len(c.Buttons) == 0 || len(c.Buttons[0]) == 0 {
			continue
		}
		if a, err := ui.Decode(c.Buttons[0][0]); err == nil && a.Kind == ui.ActWizNoop {
			return c.MsgID
		}
	}
	return 0
}

// count returns how many recorded calls satisfy f.
func (m *mockTelegram) count(f func(tgCall) bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if f(c) {
			n++
		}
	}
	return n
}

// sent returns the sendMessage calls whose text contains sub.
func (m *mockTelegram) sent(sub string) []tgCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []tgCall
	for _, c := range m.calls {
		if c.Method == "sendMessage" && strings.Contains(c.Text, sub) {
			out = append(out, c)
		}
	}
	return out
}

func (m *mockTelegram) polled(token string) bool {
	return m.count(func(c tgCall) bool { return c.Token == token && c.Method == "getUpdates" }) > 0
}

// waitUntil polls cond every few milliseconds. It is the only place where the
// tests wait in real time, and only for goroutines to catch up.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
