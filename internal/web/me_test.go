package web

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func TestMe(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	w := h.do("GET", "/api/me", "", bearer(tok))
	var me meResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := meResponse{
		User:    meUser{ID: idAnn, Username: "ann", Lang: "fa", Calendar: "jalali", TZ: "UTC", Dir: "rtl"},
		Session: meSession{Kind: "miniapp", ExpiresAt: testNow.Add(12 * time.Hour)},
		Bot:     meBot{Username: "TodoBot"},
	}
	if me.User != want.User || me.Session.Kind != want.Session.Kind || !me.Session.ExpiresAt.Equal(want.Session.ExpiresAt) || me.Bot != want.Bot {
		t.Errorf("me %+v, want %+v", me, want)
	}
	// A change of language shows on the next request (Prefs on each request).
	if err := h.svc.SetLang(context.Background(), idAnn, i18n.En); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(h.do("GET", "/api/me", "", bearer(tok)).Body.Bytes(), &me)
	if me.User.Lang != "en" || me.User.Dir != "ltr" {
		t.Errorf("after SetLang: %+v", me.User)
	}
	wantErr(t, h.do("GET", "/api/me", ""), 401, "unauthenticated")
}

func TestErrorMessageInUserLanguage(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn) // fa
	var b errBody
	_ = json.Unmarshal(h.do("GET", "/api/chats/abc/items", "", bearer(tok)).Body.Bytes(), &b)
	if b.Error["message"] != (i18n.Locale{Lang: i18n.Fa}).T("web.err.not_found") {
		t.Errorf("message %v", b.Error["message"])
	}
	_ = json.Unmarshal(h.do("GET", "/api/me", "", header("Accept-Language", "fa-IR,fa;q=0.9")).Body.Bytes(), &b)
	if b.Error["message"] != (i18n.Locale{Lang: i18n.Fa}).T("web.err.unauthenticated") {
		t.Errorf("no session, Accept-Language fa: %v", b.Error["message"])
	}
}

func TestChatsPrivateOnly(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userBob)
	// A group the bot knows does not show yet (W4).
	if err := h.st.EnsureChat(context.Background(), model.Chat{ID: -100, Kind: model.ChatGroup, Title: "Family", CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	w := h.do("GET", "/api/chats", "", bearer(tok))
	var resp chatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(resp.Chats) != 1 || resp.Chats[0] != (chatDTO{ID: idBob, Kind: "private"}) || resp.Partial {
		t.Errorf("chats %+v", resp)
	}
	wantErr(t, h.do("GET", "/api/chats", ""), 401, "unauthenticated")
}

func TestI18nEndpoint(t *testing.T) {
	h := newHarness(t)
	for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
		w := h.do("GET", "/api/i18n?lang="+string(lang), "")
		if w.Code != 200 {
			t.Fatalf("%s: %d", lang, w.Code)
		}
		var resp i18nResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		wantDir := map[i18n.Lang]string{i18n.En: "ltr", i18n.Fa: "rtl"}[lang]
		if resp.Lang != string(lang) || resp.Dir != wantDir {
			t.Errorf("%s: lang %q dir %q", lang, resp.Lang, resp.Dir)
		}
		loc := i18n.Locale{Lang: lang}
		if resp.Messages["web.app.notallowed"] != loc.T("web.app.notallowed") || resp.Messages["web.err.csrf"] != loc.T("web.err.csrf") {
			t.Errorf("%s: messages %v", lang, resp.Messages)
		}
		for k := range resp.Messages {
			if len(k) < 4 || k[:4] != "web." {
				t.Errorf("%s: key %q is not a web key", lang, k)
			}
		}
		if w.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Errorf("Cache-Control %q", w.Header().Get("Cache-Control"))
		}
		etag := w.Header().Get("ETag")
		if etag == "" {
			t.Fatal("no ETag")
		}
		w2 := h.do("GET", "/api/i18n?lang="+string(lang), "", header("If-None-Match", etag))
		if w2.Code != 304 || w2.Body.Len() != 0 {
			t.Errorf("If-None-Match: %d", w2.Code)
		}
	}
	for _, q := range []string{"", "?lang=de", "?lang=EN", "?lang="} {
		wantErr(t, h.do("GET", "/api/i18n"+q, ""), 400, "bad_lang")
	}
}
