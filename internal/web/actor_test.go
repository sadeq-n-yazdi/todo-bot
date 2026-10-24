package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// actorVia runs withActor for {chat} = chat, with a session of user, and
// returns the actor that the next handler saw.
func actorVia(t *testing.T, h *harness, user int64, chat string) (service.Actor, *httptest.ResponseRecorder, bool) {
	t.Helper()
	var got service.Actor
	called := false
	mw := h.srv.withActor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, called = actorFrom(r.Context()), true
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("GET", "/api/chats/"+chat+"/items", nil)
	r.SetPathValue("chat", chat)
	ri := &reqInfo{sess: &session{WebSession: model.WebSession{UserID: user}}}
	r = r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, ri))
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)
	return got, w, called
}

func TestActorPrivate(t *testing.T) {
	h := newHarness(t)
	a, w, called := actorVia(t, h, idAnn, "42")
	if !called || a != (service.Actor{UserID: idAnn, ChatID: idAnn, ChatKind: model.ChatPrivate}) {
		t.Fatalf("own ID without a chats row: %d %+v", w.Code, a)
	}
	if err := h.st.EnsureChat(context.Background(), model.Chat{ID: idAnn, Kind: model.ChatPrivate, Title: "Ann", CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if a, _, _ := actorVia(t, h, idAnn, "42"); a.ChatTitle != "Ann" {
		t.Errorf("title %q, want the title of the chats row", a.ChatTitle)
	}
}

func TestActorRefusals(t *testing.T) {
	h := newHarness(t)
	for _, chat := range []string{
		"43",                                             // another user's private chat
		"1",                                              // any other positive ID
		"0",                                              //
		"-100",                                           // a group: the W3 groupActor answers 404
		"-1001234567890",                                 //
		"abc", "4.2", "", "0x2a", "99999999999999999999", // not an int64
	} {
		_, w, called := actorVia(t, h, idAnn, chat)
		if called {
			t.Errorf("{chat}=%q: the handler ran", chat)
			continue
		}
		wantErr(t, w, 404, "not_found")
	}
}

// Every route under {chat} goes through the actor middleware.
func TestRoutesUnderChatUseTheActor(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	routes := []struct{ method, path string }{
		{"GET", "/api/chats/%s/items"},
		{"GET", "/api/chats/%s/members"},
		{"POST", "/api/chats/%s/items"},
		{"POST", "/api/chats/%s/items/1/done"},
		{"DELETE", "/api/chats/%s/items/1"},
		{"POST", "/api/chats/%s/items/1/remind"},
		{"GET", "/api/chats/%s/items/1"},
		{"PATCH", "/api/chats/%s/items/1"},
	}
	for _, rt := range routes {
		for chat, want := range map[string]int{"42": 501, "43": 404, "-100": 404, "x": 404} {
			p := strings.Replace(rt.path, "%s", chat, 1)
			w := h.do(rt.method, p, `{}`, bearer(tok))
			if w.Code != want {
				t.Errorf("%s %s: %d, want %d", rt.method, p, w.Code, want)
			}
		}
	}
}

// No handler reads {chat}: only actor.go calls PathValue("chat").
func TestOnlyTheActorReadsChat(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`PathValue\("chat"\)`)
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "actor.go" {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if re.Match(b) {
			t.Errorf("%s reads {chat}", name)
		}
	}
}

func TestStubRoutes(t *testing.T) {
	h := newHarness(t)
	mini := h.signIn(userAnn)
	for _, rt := range []struct {
		method, path string
		tok          string
	}{
		{"GET", "/api/chats/42/items", mini},
		{"GET", "/api/assigned", mini},
		{"POST", "/api/auth/code", mini},
		{"POST", "/api/auth/code/redeem", ""},
		{"DELETE", "/api/auth/code", mini},
		{"POST", "/api/parse", mini},
		{"GET", "/api/chats/42/members", mini},
		{"POST", "/api/chats/42/items", mini},
		{"POST", "/api/chats/42/items/1/done", mini},
		{"DELETE", "/api/chats/42/items/1", mini},
		{"POST", "/api/chats/42/items/1/remind", mini},
		{"PUT", "/api/me/settings", mini},
		{"GET", "/api/chats/42/items/1", mini},
		{"PATCH", "/api/chats/42/items/1", mini},
		{"GET", "/auth/telegram?id=42", ""},
	} {
		var opts []reqOpt
		if rt.tok != "" {
			opts = append(opts, bearer(rt.tok))
		}
		w := h.do(rt.method, rt.path, `{}`, opts...)
		if w.Code != 501 || errCode(t, w) != "not_implemented" {
			t.Errorf("%s %s: %d %s", rt.method, rt.path, w.Code, w.Body)
		}
		// The routes with a session refuse a request without one.
		if rt.tok != "" {
			wantErr(t, h.do(rt.method, rt.path, `{}`), 401, "unauthenticated")
		}
	}
}

func TestStubLimitsPerIP(t *testing.T) {
	h := newHarness(t)
	for _, rt := range []struct {
		method, path string
		n            int
	}{{"POST", "/api/auth/code/redeem", redeemPerMin}, {"GET", "/auth/telegram", widgetPerMin}} {
		for range rt.n {
			if w := h.do(rt.method, rt.path, `{}`, peer("10.7.7.7:1")); w.Code != 501 {
				t.Fatalf("%s: %d", rt.path, w.Code)
			}
		}
		wantErr(t, h.do(rt.method, rt.path, `{}`, peer("10.7.7.7:1")), 429, "rate_limited")
	}
}
