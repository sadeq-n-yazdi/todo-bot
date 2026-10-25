package web

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	e := newWriteEnv(t)
	w := e.do("POST", "/api/parse", `{"text":"buy milk tomorrow ۱۸:۰۰"}`, bearer(e.ann))
	wantStatus(t, w, 200)
	got := decodeBody[parseResponse](t, w)
	// Ann is in Asia/Tehran: 18:00 there is 14:30 UTC.
	if got.Text != "buy milk" || got.DueAt == nil || !got.DueAt.Equal(time.Date(2026, 3, 2, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("parse %+v", got)
	}
	if got.DueText == nil || !strings.Contains(*got.DueText, "۱۸:۰۰") || got.Recur != "" || got.RecurText != "" {
		t.Errorf("parse %+v", got)
	}
	// No date: the due fields are null.
	w = e.do("POST", "/api/parse", `{"text":"call mom"}`, bearer(e.ann))
	wantStatus(t, w, 200)
	if s := w.Body.String(); !strings.Contains(s, `"due_at":null`) || !strings.Contains(s, `"due_text":null`) {
		t.Errorf("body %s", s)
	}
	// A repeat. The parse writes nothing.
	w = e.do("POST", "/api/parse", `{"text":"water the plants every monday 07:00"}`, bearer(e.ann))
	wantStatus(t, w, 200)
	got = decodeBody[parseResponse](t, w)
	if got.Text != "water the plants" || got.Recur == "" || got.RecurText == "" || got.DueAt == nil || !got.DueAt.After(e.clk.Now()) {
		t.Errorf("repeat %+v", got)
	}
	if ts, _, _ := e.svc.List(t.Context(), idAnn, 0); len(ts) != 6 { // the fixture only
		t.Errorf("%d items after parse", len(ts))
	}
}

func TestParseErrors(t *testing.T) {
	e := newWriteEnv(t)
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"text":" "}`, 422, "empty_text"},
		{`{"text":"` + strings.Repeat("x", 1001) + `"}`, 422, "too_long"},
		{`{"text":"x","more":1}`, 400, "bad_request"},
	} {
		wantErr(t, e.do("POST", "/api/parse", tc.body, bearer(e.ann)), tc.status, tc.code)
	}
	wantStatus(t, e.do("POST", "/api/parse", `{"text":"`+strings.Repeat("ب", 1000)+`"}`, bearer(e.ann)), 200)
	wantErr(t, e.do("POST", "/api/parse", `{"text":"x"}`), 401, "unauthenticated")
	wantErr(t, e.do("POST", "/api/parse", `{"text":"x"}`, bearer(e.ann), delHeader("X-Todo-Request")), 403, "csrf")
}
