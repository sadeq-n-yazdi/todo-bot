package web

import (
	"context"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

func (e *writeEnv) prefsOf(id int64) (i18n.Locale, int) {
	e.t.Helper()
	p, err := e.svc.Prefs(context.Background(), id, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return p.Locale, p.KBVersion
}

func TestSettingsLanguageResetsKeyboard(t *testing.T) {
	e := newWriteEnv(t)
	ctx := context.Background()
	if err := e.svc.SetKBVersion(ctx, idAnn, 3); err != nil {
		t.Fatal(err)
	}
	w := e.do("PUT", "/api/me/settings", `{"lang":"en"}`, bearer(e.ann))
	wantStatus(t, w, 200)
	me := decodeBody[meResponse](t, w)
	if me.User.ID != idAnn || me.User.Lang != "en" || me.User.Dir != "ltr" || me.User.Calendar != "gregorian" || me.User.TZ != "Asia/Tehran" {
		t.Fatalf("me %+v", me.User)
	}
	if lc, kb := e.prefsOf(idAnn); lc.Lang != i18n.En || kb != 0 {
		t.Fatalf("lang %s, keyboard version %d; want en, 0", lc.Lang, kb)
	}
	// The same language again is no change: the version stays.
	if err := e.svc.SetKBVersion(ctx, idAnn, 3); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.do("PUT", "/api/me/settings", `{"lang":"en"}`, bearer(e.ann)), 200)
	if _, kb := e.prefsOf(idAnn); kb != 3 {
		t.Fatalf("keyboard version %d after no change, want 3", kb)
	}
	// An error after the change is in the new language.
	w = e.do("PUT", "/api/me/settings", `{"calendar":"mayan"}`, bearer(e.ann))
	wantErr(t, w, 422, "bad_calendar")
	if b := decodeBody[errBody](t, w); b.Error["message"] != i18n.Default("en").T("web.err.bad_calendar") {
		t.Errorf("message %v", b.Error["message"])
	}
}

func TestSettingsCalendarAndBoth(t *testing.T) {
	e := newWriteEnv(t)
	if err := e.svc.SetKBVersion(context.Background(), idAnn, 3); err != nil {
		t.Fatal(err)
	}
	w := e.do("PUT", "/api/me/settings", `{"calendar":"jalali"}`, bearer(e.ann))
	wantStatus(t, w, 200)
	if me := decodeBody[meResponse](t, w); me.User.Calendar != "jalali" || me.User.Lang != "fa" || me.User.Dir != "rtl" {
		t.Fatalf("me %+v", me.User)
	}
	if lc, kb := e.prefsOf(idAnn); lc.Cal != calendar.Jalali || kb != 3 {
		t.Fatalf("calendar %s, keyboard %d", lc.Cal, kb)
	}
	w = e.do("PUT", "/api/me/settings", `{"lang":"en","calendar":"gregorian"}`, bearer(e.ann))
	wantStatus(t, w, 200)
	if me := decodeBody[meResponse](t, w); me.User.Calendar != "gregorian" || me.User.Lang != "en" {
		t.Fatalf("me %+v", me.User)
	}
	if _, kb := e.prefsOf(idAnn); kb != 0 {
		t.Fatalf("keyboard %d", kb)
	}
}

func TestSettingsErrors(t *testing.T) {
	e := newWriteEnv(t)
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"lang":"de"}`, 422, "bad_lang"},
		{`{"lang":""}`, 422, "bad_lang"},
		{`{"calendar":"hijri"}`, 422, "bad_calendar"},
		// The calendar is wrong, so the language does not change either.
		{`{"lang":"en","calendar":"hijri"}`, 422, "bad_calendar"},
		{`{"lang":"de","calendar":"jalali"}`, 422, "bad_lang"},
		{`{}`, 400, "bad_request"},
		{`{"tz":"UTC"}`, 400, "bad_request"},
		{`[]`, 400, "bad_request"},
	} {
		wantErr(t, e.do("PUT", "/api/me/settings", tc.body, bearer(e.ann)), tc.status, tc.code)
	}
	if lc, _ := e.prefsOf(idAnn); lc.Lang != i18n.Fa || lc.Cal != calendar.Gregorian {
		t.Fatalf("locale changed: %+v", lc)
	}
	wantErr(t, e.do("PUT", "/api/me/settings", `{"lang":"en"}`), 401, "unauthenticated")
	wantErr(t, e.do("PUT", "/api/me/settings", `{"lang":"en"}`, bearer(e.ann), delHeader("Origin"), delHeader("Sec-Fetch-Site")), 403, "csrf")
}
