package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func TestEditMessageNotModifiedIsSuccess(t *testing.T) {
	f := newFakeAPI(t)
	f.reply("editMessageText", `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified: specified new message content and reply markup are exactly the same"}`)
	b := f.newBot(t)
	if err := b.Edit(context.Background(), 5, 9, tg.OutMessage{Text: "same"}); err != nil {
		t.Fatalf("Edit = %v, want nil", err)
	}
	// Other 400s are still errors.
	f.reply("editMessageText", `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`)
	if err := b.Edit(context.Background(), 5, 9, tg.OutMessage{Text: "x"}); err == nil {
		t.Fatal("want error for other 400")
	}
}

func TestMessageTooLong(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	ctx := context.Background()

	if _, err := b.Send(ctx, 5, tg.OutMessage{Text: strings.Repeat("a", 4096)}); err != nil {
		t.Fatalf("4096 units must be accepted: %v", err)
	}
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()

	tooLong := map[string]string{
		"ascii": strings.Repeat("a", 4097),
		"emoji": strings.Repeat("\U0001F600", 2049), // 2 UTF-16 units each = 4098
	}
	for name, text := range tooLong {
		if _, err := b.Send(ctx, 5, tg.OutMessage{Text: text}); !errors.Is(err, ErrMessageTooLong) {
			t.Errorf("%s Send err = %v", name, err)
		}
		if err := b.Edit(ctx, 5, 1, tg.OutMessage{Text: text}); !errors.Is(err, ErrMessageTooLong) {
			t.Errorf("%s Edit err = %v", name, err)
		}
	}
	if n := len(f.callsTo("sendMessage")) + len(f.callsTo("editMessageText")); n != 0 {
		t.Fatalf("API called %d times for rejected messages", n)
	}
	// 2048 emoji = exactly 4096 units is fine.
	if _, err := b.Send(ctx, 5, tg.OutMessage{Text: strings.Repeat("\U0001F600", 2048)}); err != nil {
		t.Errorf("4096 units of emoji: %v", err)
	}
}

func TestCallbackDataTooLong(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	ctx := context.Background()

	ok := tg.OutMessage{Text: "x", Keyboard: [][]tg.Button{{{Text: "a", Data: strings.Repeat("d", 64)}}}}
	if _, err := b.Send(ctx, 5, ok); err != nil {
		t.Fatalf("64 bytes must be accepted: %v", err)
	}
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()

	for name, data := range map[string]string{
		"ascii":     strings.Repeat("d", 65),
		"multibyte": strings.Repeat("ش", 33), // 2 bytes each = 66 bytes, 33 runes
	} {
		m := tg.OutMessage{Text: "x", Keyboard: [][]tg.Button{{{Text: "ok", Data: "ok"}}, {{Text: "bad", Data: data}}}}
		if _, err := b.Send(ctx, 5, m); !errors.Is(err, ErrCallbackDataTooLong) {
			t.Errorf("%s Send err = %v", name, err)
		}
		if err := b.Edit(ctx, 5, 1, m); !errors.Is(err, ErrCallbackDataTooLong) {
			t.Errorf("%s Edit err = %v", name, err)
		}
	}
	if n := len(f.callsTo("sendMessage")) + len(f.callsTo("editMessageText")); n != 0 {
		t.Fatalf("API called %d times for rejected messages", n)
	}
}

func TestServerURLValidation(t *testing.T) {
	for _, u := range []string{"ftp://example.com", "http://", "example.com/api", "http://127.0.0.1:1/%zz", "javascript:alert(1)"} {
		_, err := New(context.Background(), "123:SECRET", Options{ServerURL: u})
		if err == nil {
			t.Errorf("ServerURL %q accepted", u)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("ServerURL %q: error leaks token: %v", u, err)
		}
	}
}

func TestTokenNeverInErrors(t *testing.T) {
	const token = "123:SECRET"

	// A server that is not there: the library's own error path.
	_, err := New(context.Background(), token, Options{ServerURL: "http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("New error leaks token: %v", err)
	}

	// Any error text containing the token is redacted, and the chain survives.
	b := &Bot{token: token}
	root := errors.New("boom")
	raw := fmt.Errorf("parse %q: %w", "http://h/bot"+token+"/getMe", root)
	got := b.mapErr(raw)
	if strings.Contains(got.Error(), token) || !strings.Contains(got.Error(), "<token>") {
		t.Errorf("mapErr = %v", got)
	}
	if !errors.Is(got, root) {
		t.Error("redaction lost the error chain")
	}

	// API descriptions are redacted too.
	ae := b.mapErr(fmt.Errorf("%w, echo %s", bot.ErrorBadRequest, token))
	var apiErr *tg.APIError
	if !errors.As(ae, &apiErr) || strings.Contains(apiErr.Desc, token) {
		t.Errorf("api error = %v", ae)
	}
}

func TestAuthAndConflictErrors(t *testing.T) {
	for _, tc := range []struct {
		code int
		desc string
	}{{401, "Unauthorized"}, {409, "Conflict: terminated by other getUpdates request"}} {
		f := newFakeAPI(t)
		b := f.newBot(t)
		f.reply("sendMessage", fmt.Sprintf(`{"ok":false,"error_code":%d,"description":%q}`, tc.code, tc.desc))
		_, err := b.Send(context.Background(), 5, tg.OutMessage{Text: "x"})
		var ae *tg.APIError
		if !errors.As(err, &ae) || ae.Code != tc.code || ae.Desc != tc.desc {
			t.Errorf("code %d: err = %v (%+v)", tc.code, err, ae)
		}
		if tg.IsPermanent(err) {
			t.Errorf("code %d must not be permanent", tc.code)
		}
	}
}

func TestChannelChatIsGroup(t *testing.T) {
	f := newFakeAPI(t)
	f.queue(`{"update_id":200,"message":{"message_id":1,"date":1,"text":"hi",
		"chat":{"id":-1005,"type":"channel","title":"News"},"from":{"id":1,"is_bot":false,"first_name":"A"}}}`)
	b := f.newBot(t)
	var got tg.Update
	collect(t, b, 1, func(u tg.Update) error { got = u; return nil })
	if got.Message == nil || got.Message.ChatKind != model.ChatGroup || got.Message.ChatTitle != "News" {
		t.Fatalf("update = %+v", got)
	}
}

// A cancel during a burst must not confirm updates that were never handled:
// no getUpdates may ask for an offset beyond the last update handed to the
// handler, so Telegram redelivers the rest to the next poller.
func TestCancelDuringBurstConfirmsOnlyHandledUpdates(t *testing.T) {
	f := newFakeAPI(t)
	f.queue(privateMsg, groupMsg, migrateMsg) // ids 100, 101, 102 in one batch
	b := f.newBot(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan int64, 8)
	done := make(chan error, 1)
	go func() {
		done <- b.Poller().Run(ctx, func(c context.Context, u tg.Update) error {
			started <- u.ID
			<-c.Done() // a slow handler that honours ctx
			return c.Err()
		})
	}()

	select {
	case id := <-started:
		if id != 100 {
			t.Fatalf("first update = %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler never called")
	}
	start := time.Now()
	cancel()
	select {
	case <-done:
		if d := time.Since(start); d > time.Second {
			t.Fatalf("shutdown took %v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of cancel")
	}

	select {
	case id := <-started:
		t.Fatalf("update %d was handled after cancel", id)
	default:
	}
	for _, c := range f.callsTo("getUpdates") {
		if off := c.Form.Get("offset"); off != "" {
			if n, _ := strconv.Atoi(off); n > 101 {
				t.Fatalf("offset %d confirms unhandled updates", n)
			}
		}
	}
}
