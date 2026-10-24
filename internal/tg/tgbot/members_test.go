package tgbot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func memberBody(status, extra string) string {
	return `{"ok":true,"result":{"status":"` + status + `","user":{"id":9,"is_bot":false,"first_name":"U"}` + extra + `}}`
}

func TestChatMemberStatuses(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		want  tg.ChatMember
		isErr bool
	}{
		{"creator", memberBody("creator", `,"is_anonymous":false`), tg.ChatMember{Status: tg.MemberCreator}, false},
		{"administrator", memberBody("administrator", `,"can_be_edited":false`), tg.ChatMember{Status: tg.MemberAdministrator}, false},
		{"member", memberBody("member", ""), tg.ChatMember{Status: tg.MemberMember}, false},
		{"restricted in", memberBody("restricted", `,"is_member":true`), tg.ChatMember{Status: tg.MemberRestricted, IsMember: true}, false},
		{"restricted out", memberBody("restricted", `,"is_member":false`), tg.ChatMember{Status: tg.MemberRestricted}, false},
		{"left", memberBody("left", ""), tg.ChatMember{Status: tg.MemberLeft}, false},
		{"kicked", memberBody("kicked", `,"until_date":0`), tg.ChatMember{Status: tg.MemberKicked}, false},
		{"unknown", memberBody("zombie", ""), tg.ChatMember{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAPI(t)
			b := f.newBot(t)
			f.reply("getChatMember", tc.body)
			got, err := b.ChatMember(context.Background(), -1001, 9)
			if (err != nil) != tc.isErr {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			c := f.callsTo("getChatMember")
			if len(c) != 1 || c[0].Form.Get("chat_id") != "-1001" || c[0].Form.Get("user_id") != "9" {
				t.Errorf("calls = %+v", c)
			}
		})
	}
}

func TestChatMemberErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		code    int
		desc    string
		migrate int64
	}{
		{"user not found", `{"ok":false,"error_code":400,"description":"Bad Request: user not found"}`, 400, "user not found", 0},
		{"member not found", `{"ok":false,"error_code":400,"description":"Bad Request: member not found"}`, 400, "member not found", 0},
		{"chat not found", `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, 400, "chat not found", 0},
		{"bot kicked", `{"ok":false,"error_code":403,"description":"Forbidden: bot was kicked from the group chat"}`, 403, "bot was kicked", 0},
		{"migrated", `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-100777}}`, 400, "upgraded", -100777},
		{"rate limit", `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 3","parameters":{"retry_after":3}}`, 429, "retry after 3", 0},
		{"server", `{"ok":false,"error_code":502,"description":"Bad Gateway"}`, 502, "Bad Gateway", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAPI(t)
			b := f.newBot(t)
			f.reply("getChatMember", tc.body)
			_, err := b.ChatMember(context.Background(), -1001, 9)
			var ae *tg.APIError
			if !errors.As(err, &ae) {
				t.Fatalf("err = %v, want *tg.APIError", err)
			}
			if ae.Code != tc.code || ae.MigrateTo != tc.migrate || !strings.Contains(ae.Desc, tc.desc) {
				t.Errorf("got %+v", *ae)
			}
		})
	}
}

func TestChatMemberContextCancelIsNotAPIError(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.ChatMember(ctx, -1001, 9)
	var ae *tg.APIError
	if err == nil || errors.As(err, &ae) {
		t.Fatalf("err = %v", err)
	}
}

type wireButton struct {
	Text   string `json:"text"`
	Data   string `json:"callback_data"`
	URL    string `json:"url"`
	WebApp *struct {
		URL string `json:"url"`
	} `json:"web_app"`
}

func TestSendURLAndWebAppButtons(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	_, err := b.Send(context.Background(), 5, tg.OutMessage{
		Text: "x",
		Keyboard: [][]tg.Button{{
			{Text: "Open", WebAppURL: "https://todo.example.com/app"},
			{Text: "Link", URL: "https://todo.example.com/login"},
			{Text: "Done", Data: "done:3"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var kb struct {
		Rows [][]wireButton `json:"inline_keyboard"`
	}
	raw := f.callsTo("sendMessage")[0].Form.Get("reply_markup")
	if err := json.Unmarshal([]byte(raw), &kb); err != nil {
		t.Fatalf("%q: %v", raw, err)
	}
	r := kb.Rows[0]
	if r[0].WebApp == nil || r[0].WebApp.URL != "https://todo.example.com/app" || r[0].URL != "" || r[0].Data != "" {
		t.Errorf("web_app button = %+v", r[0])
	}
	if r[1].URL != "https://todo.example.com/login" || r[1].WebApp != nil || r[1].Data != "" {
		t.Errorf("url button = %+v", r[1])
	}
	if r[2].Data != "done:3" || r[2].URL != "" || r[2].WebApp != nil {
		t.Errorf("data button = %+v", r[2])
	}
}

func TestEditURLButtons(t *testing.T) {
	f := newFakeAPI(t)
	b := f.newBot(t)
	err := b.Edit(context.Background(), 5, 1, tg.OutMessage{Text: "x", Keyboard: [][]tg.Button{{{Text: "L", URL: "https://a.example/x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	raw := f.callsTo("editMessageText")[0].Form.Get("reply_markup")
	if !strings.Contains(raw, `"url":"https://a.example/x"`) {
		t.Errorf("reply_markup = %q", raw)
	}
}

func TestButtonWithMoreThanOneKindIsRejected(t *testing.T) {
	tests := []struct {
		name string
		btn  tg.Button
	}{
		{"data and url", tg.Button{Text: "x", Data: "d", URL: "https://a.example"}},
		{"data and web app", tg.Button{Text: "x", Data: "d", WebAppURL: "https://a.example"}},
		{"url and web app", tg.Button{Text: "x", URL: "https://a.example", WebAppURL: "https://a.example"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAPI(t)
			b := f.newBot(t)
			m := tg.OutMessage{Text: "x", Keyboard: [][]tg.Button{{tc.btn}}}
			_, err := b.Send(context.Background(), 5, m)
			if !errors.Is(err, ErrButtonKind) {
				t.Errorf("Send err = %v", err)
			}
			if err := b.Edit(context.Background(), 5, 1, m); !errors.Is(err, ErrButtonKind) {
				t.Errorf("Edit err = %v", err)
			}
			if n := len(f.callsTo("sendMessage")) + len(f.callsTo("editMessageText")); n != 0 {
				t.Errorf("%d API calls made", n)
			}
		})
	}
}
