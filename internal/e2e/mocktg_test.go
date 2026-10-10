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
}

// tgCall is one recorded Bot API call.
type tgCall struct {
	Token, Method string
	ChatID        int64
	Text          string
	Offset        int64
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
	m.mu.Lock()
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
		m.nextMsg++
		id := m.nextMsg
		m.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":` + strconv.Itoa(id) +
			`,"date":1,"chat":{"id":` + strconv.FormatInt(c.ChatID, 10) + `,"type":"private"}}}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}
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
	b, _ := json.Marshal(map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id": updateID,
			"date":       1,
			"chat":       map[string]any{"id": user, "type": "private"},
			"from":       map[string]any{"id": user, "is_bot": false, "first_name": "U"},
			"text":       text,
		},
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, b)
	m.ids = append(m.ids, updateID)
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
