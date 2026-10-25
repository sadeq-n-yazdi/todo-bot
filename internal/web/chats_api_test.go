package web

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func listChats(t *testing.T, h *harness, tok string) chatsResponse {
	t.Helper()
	w := h.do("GET", "/api/chats", "", bearer(tok))
	var resp chatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return resp
}

func ids(r chatsResponse) []int64 {
	out := make([]int64, 0, len(r.Chats))
	for _, c := range r.Chats {
		out = append(out, c.ID)
	}
	return out
}

func sameIDs(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestChatsListsConfirmedMembersSortedByTitle(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	addGroup(t, h, -10, "Zoo")
	addGroup(t, h, -11, "Apples")
	addGroup(t, h, -12, "Left group")
	addGroup(t, h, -13, "Mango")
	h.members.Set(-10, idAnn, tg.ChatMember{Status: tg.MemberMember})
	h.members.Set(-11, idAnn, tg.ChatMember{Status: tg.MemberAdministrator})
	h.members.Set(-12, idAnn, tg.ChatMember{Status: tg.MemberLeft})
	h.members.Set(-13, idAnn, tg.ChatMember{Status: tg.MemberRestricted, IsMember: true})
	r := listChats(t, h, tok)
	if !sameIDs(ids(r), idAnn, -11, -13, -10) || r.Partial {
		t.Fatalf("list %+v", r)
	}
	if r.Chats[0].Kind != "private" || r.Chats[1].Kind != "group" || r.Chats[1].Title != "Apples" {
		t.Errorf("list %+v", r.Chats)
	}
	// A second list uses the fresh rows: no new call.
	n := len(h.members.Calls())
	listChats(t, h, tok)
	if len(h.members.Calls()) != n {
		t.Errorf("fresh rows caused %d calls", len(h.members.Calls())-n)
	}
}

func TestChatsCandidatesComeFromChatsOnly(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	// A member row for a chat that has no chats row is never a candidate,
	// and never causes a call.
	setRow(t, h, -500, idAnn, model.MemberYes)
	// A hint adds a group that has no items yet.
	addGroup(t, h, -501, "Hinted")
	if err := h.svc.NoteMember(context.Background(), hintActorOf(idAnn, -501)); err != nil {
		t.Fatal(err)
	}
	h.members.Set(-501, idAnn, tg.ChatMember{Status: tg.MemberMember})
	r := listChats(t, h, tok)
	if !sameIDs(ids(r), idAnn, -501) {
		t.Fatalf("list %+v", r)
	}
	for _, c := range h.members.Calls() {
		if c.ChatID == -500 {
			t.Error("called Telegram for a chat that is not in chats")
		}
	}
}

// A hint never grants access: with no confirmation the group does not show.
func TestChatsHintDoesNotGrant(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	addGroup(t, h, -501, "Hinted")
	if err := h.svc.NoteMember(context.Background(), hintActorOf(idAnn, -501)); err != nil {
		t.Fatal(err)
	}
	h.members.Set(-501, idAnn, tg.ChatMember{Status: tg.MemberLeft})
	if r := listChats(t, h, tok); !sameIDs(ids(r), idAnn) {
		t.Fatalf("list %+v", r)
	}
}

func TestChatsPartialAndGrace(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	addGroup(t, h, -20, "Graced")
	addGroup(t, h, -21, "Stranger") // no row: no grace
	addGroup(t, h, -22, "Good")
	setRow(t, h, -20, idAnn, model.MemberYes)
	h.clk.Advance(30 * time.Minute)
	h.members.SetErr(-20, idAnn, &tg.APIError{Code: 500, Desc: "down"})
	h.members.SetErr(-21, idAnn, &tg.APIError{Code: 500, Desc: "down"})
	h.members.Set(-22, idAnn, tg.ChatMember{Status: tg.MemberMember})
	r := listChats(t, h, tok)
	if !sameIDs(ids(r), idAnn, -22, -20) || !r.Partial {
		t.Fatalf("list %+v", r)
	}
}

func TestChatsAtMostFourChecksAtOnce(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for i := range 10 {
		id := -100 - int64(i)
		addGroup(t, h, id, "G")
		h.members.Set(id, idAnn, tg.ChatMember{Status: tg.MemberMember})
	}
	var cur, peak atomic.Int32
	h.members.Hook = func(context.Context, int64, int64) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
	}
	r := listChats(t, h, tok)
	if len(r.Chats) != 11 {
		t.Fatalf("list %+v", r)
	}
	if p := peak.Load(); p != 4 {
		t.Errorf("peak %d checks at the same time, want 4", p)
	}
}

func TestChatsStopAfterTheTimeout(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	h.srv.members.listTimeout = 50 * time.Millisecond
	h.srv.members.callTimeout = 10 * time.Second
	addGroup(t, h, -30, "Slow")
	addGroup(t, h, -31, "Graced")
	setRow(t, h, -31, idAnn, model.MemberYes)
	h.clk.Advance(30 * time.Minute)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	h.members.Hook = func(ctx context.Context, chat, _ int64) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	h.members.Default = tg.ChatMember{Status: tg.MemberMember}
	start := time.Now()
	r := listChats(t, h, tok)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
	if !sameIDs(ids(r), idAnn, -31) || !r.Partial {
		t.Fatalf("list %+v", r)
	}
	once.Do(func() { close(release) })
}

func TestChatsWithoutGroups(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userBob)
	r := listChats(t, h, tok)
	if len(r.Chats) != 1 || r.Chats[0] != (chatDTO{ID: idBob, Kind: "private"}) || r.Partial {
		t.Fatalf("list %+v", r)
	}
}

func TestChatsUnexpectedFailureIsPartial(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	addGroup(t, h, -40, "G")
	h.members.SetErr(-40, idAnn, errors.New("boom"))
	r := listChats(t, h, tok)
	if !sameIDs(ids(r), idAnn) || !r.Partial {
		t.Fatalf("list %+v", r)
	}
}
