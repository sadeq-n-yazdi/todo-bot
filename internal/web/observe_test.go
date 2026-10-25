package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func TestObservePassesThrough(t *testing.T) {
	h := newHarness(t)
	want := errors.New("next failed")
	var seen tg.Update
	next := func(_ context.Context, u tg.Update) error { seen = u; return want }
	wrapped := Observe(next, h.svc, map[int64]bool{idAnn: true}, nil)
	if err := wrapped(context.Background(), tg.Update{ID: 7}); !errors.Is(err, want) || seen.ID != 7 {
		t.Fatalf("err %v, update %+v", err, seen)
	}
}

func newObs(h *harness, next tg.Handler) tg.Handler {
	return Observe(next, h.svc, map[int64]bool{idAnn: true}, nil)
}

func okNext(context.Context, tg.Update) error { return nil }

func groupMsg(from int64, title string) tg.Update {
	return tg.Update{ID: 1, Message: &tg.Message{ChatID: -1001, ChatKind: model.ChatGroup, ChatTitle: title, FromID: from, Text: "hi"}}
}

func TestObserveWritesHintForAllowedUserInGroup(t *testing.T) {
	h := newHarness(t)
	obs := newObs(h, okNext)
	if err := obs(context.Background(), groupMsg(idAnn, "Family")); err != nil {
		t.Fatal(err)
	}
	m := rowOf(t, h, -1001, idAnn)
	if m.Status != model.MemberUnknown || !m.SeenAt.Equal(h.clk.Now()) || !m.CheckedAt.IsZero() {
		t.Errorf("row %+v", m)
	}
	c, err := h.svc.Chat(context.Background(), -1001)
	if err != nil || c.Title != "Family" || c.Kind != model.ChatGroup {
		t.Errorf("chat %+v, %v", c, err)
	}
	// A hint never grants access.
	h.members.Set(-1001, idAnn, tg.ChatMember{Status: tg.MemberLeft})
	wantAE(t, h.actor(-1001), 403, "not_member")
}

func TestObserveAtMostOncePerHour(t *testing.T) {
	h := newHarness(t)
	obs := newObs(h, okNext)
	_ = obs(context.Background(), groupMsg(idAnn, "Family"))
	first := rowOf(t, h, -1001, idAnn).SeenAt
	h.clk.Advance(59 * time.Minute)
	_ = obs(context.Background(), groupMsg(idAnn, "Family"))
	if got := rowOf(t, h, -1001, idAnn).SeenAt; !got.Equal(first) {
		t.Errorf("seen_at moved inside the hour: %v", got)
	}
	h.clk.Advance(2 * time.Minute)
	_ = obs(context.Background(), groupMsg(idAnn, "Family"))
	if got := rowOf(t, h, -1001, idAnn).SeenAt; !got.After(first) {
		t.Errorf("seen_at did not move after the hour: %v", got)
	}
}

func TestObserveIgnores(t *testing.T) {
	h := newHarness(t)
	obs := newObs(h, okNext)
	for name, u := range map[string]tg.Update{
		"user not allowed": groupMsg(idEve, "Family"),
		"private chat":     {Message: &tg.Message{ChatID: idAnn, ChatKind: model.ChatPrivate, FromID: idAnn}},
		"migration":        {Message: &tg.Message{ChatID: -1001, ChatKind: model.ChatGroup, FromID: idAnn, MigrateToChatID: -1009}},
		"private callback": {Callback: &tg.Callback{ChatID: idAnn, FromID: idAnn}},
		"callback of Eve":  {Callback: &tg.Callback{ChatID: -1001, FromID: idEve}},
		"empty update":     {},
	} {
		if err := obs(context.Background(), u); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, u := range []int64{idAnn, idEve} {
		if rows, err := h.svc.MembershipsOf(context.Background(), u); err != nil || len(rows) != 0 {
			t.Errorf("rows of %d: %+v, %v", u, rows, err)
		}
	}
}

func TestObserveCallback(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, -1001, "Family")
	obs := newObs(h, okNext)
	if err := obs(context.Background(), tg.Update{Callback: &tg.Callback{ChatID: -1001, FromID: idAnn}}); err != nil {
		t.Fatal(err)
	}
	if m := rowOf(t, h, -1001, idAnn); m.Status != model.MemberUnknown {
		t.Errorf("row %+v", m)
	}
	if c, _ := h.svc.Chat(context.Background(), -1001); c.Title != "Family" {
		t.Errorf("a callback erased the title: %q", c.Title)
	}
}

func TestObserveSkipsWhenNextFails(t *testing.T) {
	h := newHarness(t)
	boom := errors.New("mark update failed")
	obs := newObs(h, func(context.Context, tg.Update) error { return boom })
	if err := obs(context.Background(), groupMsg(idAnn, "Family")); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if _, err := h.svc.Membership(context.Background(), -1001, idAnn); err == nil {
		t.Error("a hint was written after an error of next")
	}
}

// A failed hint only gives a warning: the update itself succeeded.
func TestObserveSurvivesStoreError(t *testing.T) {
	h := newHarness(t)
	runs := 0
	obs := newObs(h, func(context.Context, tg.Update) error { runs++; return nil })
	_ = h.st.Close()
	if err := obs(context.Background(), groupMsg(idAnn, "Family")); err != nil {
		t.Fatalf("err %v", err)
	}
	if runs != 1 {
		t.Errorf("next ran %d times", runs)
	}
}
