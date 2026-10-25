package web

import (
	"context"
	"testing"
)

func TestMembersPrivateAndGroup(t *testing.T) {
	e := newWriteEnv(t)
	// Users with a known @username: ann and bob (allowlisted), and eve (not).
	if err := e.st.SetUsername(context.Background(), 777, "eve"); err != nil {
		t.Fatal(err)
	}
	for _, chat := range []int64{idAnn, groupFamily} {
		w := e.do("GET", chatPath(chat, "/members"), "", bearer(e.ann))
		wantStatus(t, w, 200)
		got := decodeBody[membersResponse](t, w)
		if len(got.Members) != 1 || got.Members[0] != (memberDTO{ID: idBob, Username: "bob"}) {
			t.Errorf("chat %d: members %+v", chat, got.Members)
		}
	}
	// Bob sees Ann, not himself.
	w := e.do("GET", chatPath(idBob, "/members"), "", bearer(e.bob))
	wantStatus(t, w, 200)
	if got := decodeBody[membersResponse](t, w); len(got.Members) != 1 || got.Members[0].ID != idAnn {
		t.Errorf("members of bob %+v", got.Members)
	}
}

func TestMembersEmptyIsAList(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.browser(idBob)
	w := h.do("GET", chatPath(idBob, "/members"), "", bearer(tok))
	wantStatus(t, w, 200)
	if got := w.Body.String(); got != "{\"members\":[]}\n" {
		t.Errorf("body %q", got)
	}
	wantErr(t, h.do("GET", chatPath(idBob, "/members"), ""), 401, "unauthenticated")
}
