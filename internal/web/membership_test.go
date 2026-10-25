package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

const grp = int64(-1001)

// addGroup makes a chats row of kind group.
func addGroup(t *testing.T, h *harness, id int64, title string) {
	t.Helper()
	c := model.Chat{ID: id, Kind: model.ChatGroup, Title: title, CreatedAt: h.clk.Now()}
	if err := h.st.EnsureChat(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

// setRow stores a chat_members row checked at the fake time now.
func setRow(t *testing.T, h *harness, chat, user int64, st model.MemberStatus) {
	t.Helper()
	m := model.ChatMember{ChatID: chat, UserID: user, Status: st, CheckedAt: h.clk.Now()}
	if err := h.svc.RecordMembership(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func rowOf(t *testing.T, h *harness, chat, user int64) model.ChatMember {
	t.Helper()
	m, err := h.svc.Membership(context.Background(), chat, user)
	if err != nil {
		t.Fatalf("membership %d/%d: %v", chat, user, err)
	}
	return m
}

func wantAE(t *testing.T, err error, status int, code string) *apiError {
	t.Helper()
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != status || ae.Code != code {
		t.Fatalf("error %v, want %d %s", err, status, code)
	}
	return ae
}

func (h *harness) actor(chat int64) error {
	_, err := h.srv.groupActor(context.Background(), idAnn, chat)
	return err
}

func TestGroupActorNoChatsRow(t *testing.T) {
	h := newHarness(t)
	wantAE(t, h.actor(grp), 404, "not_found")
	if n := len(h.members.Calls()); n != 0 {
		t.Errorf("%d calls to Telegram for an unknown group", n)
	}
	// A chats row that is not a group is not a group either.
	if err := h.st.EnsureChat(context.Background(), model.Chat{ID: -5, Kind: model.ChatPrivate, CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	wantAE(t, h.actor(-5), 404, "not_found")
}

func TestGroupActorTitleFromChatsRow(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	setRow(t, h, grp, idAnn, model.MemberYes)
	a, err := h.srv.groupActor(context.Background(), idAnn, grp)
	if err != nil {
		t.Fatal(err)
	}
	if a.UserID != idAnn || a.ChatID != grp || a.ChatKind != model.ChatGroup || a.ChatTitle != "Family" {
		t.Errorf("actor %+v", a)
	}
}

func TestFreshMemberRowNeedsNoCall(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	setRow(t, h, grp, idAnn, model.MemberYes)
	h.clk.Advance(9 * time.Minute)
	if err := h.actor(grp); err != nil {
		t.Fatal(err)
	}
	if n := len(h.members.Calls()); n != 0 {
		t.Fatalf("%d calls with a fresh row", n)
	}
	// Past the TTL, one call refreshes the row.
	h.clk.Advance(2 * time.Minute)
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberMember})
	if err := h.actor(grp); err != nil {
		t.Fatal(err)
	}
	if n := len(h.members.Calls()); n != 1 {
		t.Fatalf("%d calls after the TTL", n)
	}
	if got := rowOf(t, h, grp, idAnn); !got.CheckedAt.Equal(h.clk.Now()) {
		t.Errorf("checked_at %v, want now", got.CheckedAt)
	}
	if err := h.actor(grp); err != nil || len(h.members.Calls()) != 1 {
		t.Fatalf("the refreshed row is fresh: %v, %d calls", err, len(h.members.Calls()))
	}
}

func TestLeftRowNeedsNoCallForAnHour(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	setRow(t, h, grp, idAnn, model.MemberLeft)
	h.clk.Advance(59 * time.Minute)
	wantAE(t, h.actor(grp), 403, "not_member")
	if n := len(h.members.Calls()); n != 0 {
		t.Fatalf("%d calls with a fresh left row", n)
	}
	h.clk.Advance(2 * time.Minute)
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberMember})
	if err := h.actor(grp); err != nil {
		t.Fatalf("after leftTTL the user is back: %v", err)
	}
	if n := len(h.members.Calls()); n != 1 {
		t.Fatalf("%d calls", n)
	}
}

// A hint newer than the last check makes the row stale (seen_at > checked_at).
func TestHintMakesRowStale(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	for _, st := range []model.MemberStatus{model.MemberYes, model.MemberLeft} {
		setRow(t, h, grp, idAnn, st)
		h.clk.Advance(61 * time.Minute) // NoteMember writes once per hour
		a := hintActorOf(idAnn, grp)
		if err := h.svc.NoteMember(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		before := len(h.members.Calls())
		h.clk.Advance(time.Second)
		h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberLeft})
		wantAE(t, h.actor(grp), 403, "not_member")
		if len(h.members.Calls()) != before+1 {
			t.Fatalf("status %s: a stale row did not cause one call", st)
		}
		if row := rowOf(t, h, grp, idAnn); row.Status != model.MemberLeft || !row.CheckedAt.After(row.SeenAt) {
			t.Fatalf("row after the check: %+v", row)
		}
	}
}

func TestUnknownRowCallsTelegram(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	if err := h.svc.NoteMember(context.Background(), hintActorOf(idAnn, grp)); err != nil {
		t.Fatal(err)
	}
	if row := rowOf(t, h, grp, idAnn); row.Status != model.MemberUnknown {
		t.Fatalf("hint row %+v", row)
	}
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberAdministrator})
	if err := h.actor(grp); err != nil {
		t.Fatal(err)
	}
	if row := rowOf(t, h, grp, idAnn); row.Status != model.MemberYes {
		t.Fatalf("row %+v", row)
	}
}

// The table of §4.4, row by row.
func TestClassificationTable(t *testing.T) {
	api := func(code int, desc string) error { return &tg.APIError{Code: code, Desc: desc} }
	tests := []struct {
		name   string
		answer *tg.ChatMember
		err    error
		status int
		code   string
		row    model.MemberStatus // "" = no row written
	}{
		{name: "d creator", answer: &tg.ChatMember{Status: tg.MemberCreator}, row: model.MemberYes},
		{name: "d administrator", answer: &tg.ChatMember{Status: tg.MemberAdministrator}, row: model.MemberYes},
		{name: "d member", answer: &tg.ChatMember{Status: tg.MemberMember}, row: model.MemberYes},
		{name: "e restricted member", answer: &tg.ChatMember{Status: tg.MemberRestricted, IsMember: true}, row: model.MemberYes},
		{name: "f restricted not member", answer: &tg.ChatMember{Status: tg.MemberRestricted}, status: 403, code: "not_member", row: model.MemberLeft},
		{name: "f left", answer: &tg.ChatMember{Status: tg.MemberLeft}, status: 403, code: "not_member", row: model.MemberLeft},
		{name: "f kicked", answer: &tg.ChatMember{Status: tg.MemberKicked}, status: 403, code: "not_member", row: model.MemberLeft},
		{name: "b user not found", err: api(400, "Bad Request: user not found"), status: 403, code: "not_member", row: model.MemberLeft},
		{name: "b member not found", err: api(400, "Bad Request: member not found"), status: 403, code: "not_member", row: model.MemberLeft},
		{name: "b participant", err: api(400, "Bad Request: PARTICIPANT_ID_INVALID"), status: 403, code: "not_member", row: model.MemberLeft},
		{name: "b user id", err: api(400, "Bad Request: USER_ID_INVALID"), status: 403, code: "not_member", row: model.MemberLeft},
		{name: "b deactivated", err: api(403, "Forbidden: user is deactivated"), status: 403, code: "not_member", row: model.MemberLeft},
		{name: "c bot kicked", err: api(403, "Forbidden: bot was kicked from the group chat"), status: 404, code: "not_found"},
		{name: "c other 403", err: api(403, "Forbidden: something"), status: 404, code: "not_found"},
		{name: "c chat not found", err: api(400, "Bad Request: chat not found"), status: 404, code: "not_found"},
		{name: "g 429", err: &tg.APIError{Code: 429, Desc: "Too Many Requests", RetryAfter: time.Second}, status: 503, code: "membership_unverified"},
		{name: "g 500", err: api(500, "Internal"), status: 503, code: "membership_unverified"},
		{name: "g other 400", err: api(400, "Bad Request: strange"), status: 503, code: "membership_unverified"},
		{name: "g network", err: errors.New("dial tcp: i/o timeout"), status: 503, code: "membership_unverified"},
		{name: "g unknown status", answer: &tg.ChatMember{Status: "weird"}, status: 503, code: "membership_unverified"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			addGroup(t, h, grp, "Family")
			// Another user's row must not change on a chat error of row c
			// being "every row of the chat": check that it does change.
			setRow(t, h, grp, idBob, model.MemberYes)
			if tc.answer != nil {
				h.members.Set(grp, idAnn, *tc.answer)
			} else {
				h.members.SetErr(grp, idAnn, tc.err)
			}
			err := h.actor(grp)
			if tc.status == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				wantAE(t, err, tc.status, tc.code)
			}
			if tc.code == "not_found" {
				if b := rowOf(t, h, grp, idBob); b.Status != model.MemberLeft {
					t.Errorf("row c must make every row of the chat left: %+v", b)
				}
				return
			}
			m, rerr := h.svc.Membership(context.Background(), grp, idAnn)
			if tc.row == "" {
				if rerr == nil {
					t.Errorf("a row was written: %+v", m)
				}
				return
			}
			if rerr != nil || m.Status != tc.row {
				t.Errorf("row %+v (%v), want %s", m, rerr, tc.row)
			}
		})
	}
}

func TestUserErrorBeatsChatError(t *testing.T) {
	// Row b comes before row c: a 403 about the user is not "the bot left".
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	setRow(t, h, grp, idBob, model.MemberYes)
	h.members.SetErr(grp, idAnn, &tg.APIError{Code: 403, Desc: "Forbidden: user is deactivated"})
	wantAE(t, h.actor(grp), 403, "not_member")
	if b := rowOf(t, h, grp, idBob); b.Status != model.MemberYes {
		t.Errorf("the row of another user changed: %+v", b)
	}
}

func TestMigrateTo(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	setRow(t, h, grp, idAnn, model.MemberYes)
	setRow(t, h, grp, idBob, model.MemberYes)
	h.clk.Advance(time.Hour)
	const newID = int64(-1009)
	h.members.SetErr(grp, idAnn, &tg.APIError{Code: 400, Desc: "Bad Request: group chat was upgraded to a supergroup chat", MigrateTo: newID})
	ae := wantAE(t, h.actor(grp), 409, "group_moved")
	if ae.Extra["new_id"] != newID {
		t.Errorf("extra %v", ae.Extra)
	}
	if _, err := h.st.GetChat(context.Background(), newID); err != nil {
		t.Errorf("the chat did not move: %v", err)
	}
	if _, err := h.st.GetChat(context.Background(), grp); err == nil {
		t.Error("the old chats row is still there")
	}
	for _, u := range []int64{idAnn, idBob} {
		if r := rowOf(t, h, grp, u); r.Status != model.MemberLeft {
			t.Errorf("old row of %d: %+v", u, r)
		}
	}
	// The old ID is not a chat any more.
	wantAE(t, h.actor(grp), 404, "not_found")
	// The new ID is a candidate of the list.
	h.members.Set(newID, idAnn, tg.ChatMember{Status: tg.MemberMember})
	tok := h.signIn(userAnn)
	got := listChats(t, h, tok)
	if len(got.Chats) != 2 || got.Chats[1].ID != newID {
		t.Errorf("list %+v", got)
	}
}

func TestOneCallForEachPair(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	release := make(chan struct{})
	entered := make(chan struct{}, 16)
	h.members.Hook = func(ctx context.Context, _, _ int64) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberMember})
	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() { errs <- h.actor(grp) }()
	}
	<-entered // the first call is in flight
	// Wait until the other requests have joined the flight, then release.
	waitFor(t, func() bool { return h.srv.members.waiters(pairKey{grp, idAnn}) == n })
	close(release)
	for range n {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if c := len(h.members.Calls()); c != 1 {
		t.Fatalf("%d calls for %d requests", c, n)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 1000 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestNodeLimit(t *testing.T) {
	h := newHarness(t)
	// Each pair is a new pair, so each request needs its own call.
	for i := range memberCallsPerMin {
		id := -2000 - int64(i)
		addGroup(t, h, id, "G")
		h.members.Set(id, idAnn, tg.ChatMember{Status: tg.MemberMember})
		if err := h.actor(id); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	over := int64(-3000)
	addGroup(t, h, over, "Over")
	h.members.Set(over, idAnn, tg.ChatMember{Status: tg.MemberMember})
	wantAE(t, h.actor(over), 503, "membership_unverified")
	if c := len(h.members.Calls()); c != memberCallsPerMin {
		t.Fatalf("%d calls reached Telegram, want %d", c, memberCallsPerMin)
	}
	if _, err := h.svc.Membership(context.Background(), over, idAnn); err == nil {
		t.Error("the limit wrote a row")
	}
	h.clk.Advance(time.Minute)
	if err := h.actor(over); err != nil {
		t.Fatalf("after a minute: %v", err)
	}
}

func TestGraceWindow(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	h.members.SetErr(grp, idAnn, &tg.APIError{Code: 502, Desc: "Bad Gateway"})
	setRow(t, h, grp, idAnn, model.MemberYes)

	h.clk.Advance(30 * time.Minute) // past the TTL, inside the grace
	if err := h.actor(grp); err != nil {
		t.Fatalf("inside the grace window: %v", err)
	}
	if r := rowOf(t, h, grp, idAnn); r.CheckedAt.After(h.clk.Now().Add(-29 * time.Minute)) {
		t.Errorf("a temporary error wrote checked_at: %+v", r)
	}
	h.clk.Advance(31 * time.Minute) // 61 minutes since the check
	wantAE(t, h.actor(grp), 503, "membership_unverified")

	// A left row never gets a grace.
	setRow(t, h, grp, idAnn, model.MemberLeft)
	h.clk.Advance(2 * time.Hour)
	wantAE(t, h.actor(grp), 503, "membership_unverified")
	// No row never gets a grace.
	addGroup(t, h, -77, "Other")
	h.members.SetErr(-77, idAnn, errors.New("down"))
	wantAE(t, h.actor(-77), 503, "membership_unverified")
}

func TestCallTimeout(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	h.srv.members.callTimeout = 30 * time.Millisecond
	h.members.Hook = func(ctx context.Context, _, _ int64) { <-ctx.Done() }
	h.members.Default = tg.ChatMember{Status: tg.MemberMember}
	h.members.Err = nil
	// The fake ignores ctx after the hook, so make the answer an error as a
	// real client does when its context ends.
	h.members.SetErr(grp, idAnn, context.DeadlineExceeded)
	start := time.Now()
	wantAE(t, h.actor(grp), 503, "membership_unverified")
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

// Over HTTP, the actor errors reach the client with their codes.
func TestGroupRouteErrors(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	addGroup(t, h, grp, "Family")
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberMember})
	if w := h.do("GET", "/api/chats/-1001/items", "", bearer(tok)); w.Code != http.StatusOK {
		t.Errorf("member: %d %s", w.Code, w.Body)
	}
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberKicked})
	h.clk.Advance(time.Hour)
	wantErr(t, h.do("GET", "/api/chats/-1001/items", "", bearer(tok)), 403, "not_member")
	h.members.SetErr(grp, idAnn, &tg.APIError{Code: 400, Desc: "x", MigrateTo: -1009})
	h.clk.Advance(2 * time.Hour)
	w := h.do("GET", "/api/chats/-1001/items", "", bearer(tok))
	wantErr(t, w, 409, "group_moved")
	if !strings.Contains(w.Body.String(), `"new_id":-1009`) {
		t.Errorf("body %s", w.Body)
	}
}

func hintActorOf(user, chat int64) service.Actor {
	return service.Actor{UserID: user, ChatID: chat, ChatKind: model.ChatGroup, ChatTitle: "Family"}
}

// When the node budget is used up, the grace rule does not apply: a user
// who left must not keep access because someone drained the budget.
func TestLimitGivesNoGrace(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	setRow(t, h, grp, idAnn, model.MemberYes)
	h.clk.Advance(30 * time.Minute) // past the TTL, inside the grace window
	for i := range memberCallsPerMin {
		id := -2000 - int64(i)
		addGroup(t, h, id, "G")
		h.members.Set(id, idAnn, tg.ChatMember{Status: tg.MemberMember})
		if err := h.actor(id); err != nil {
			t.Fatal(err)
		}
	}
	wantAE(t, h.actor(grp), 503, "membership_unverified")
	// The same row is allowed when Telegram (not our budget) is down.
	h.clk.Advance(time.Minute)
	h.members.SetErr(grp, idAnn, &tg.APIError{Code: 500, Desc: "down"})
	if err := h.actor(grp); err != nil {
		t.Fatalf("grace on a Telegram outage: %v", err)
	}
}

// The checks of GET /api/chats have their own budget.
func TestListDoesNotUseTheActorBudget(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for i := range 40 {
		id := -4000 - int64(i)
		addGroup(t, h, id, "G")
		h.members.Set(id, idAnn, tg.ChatMember{Status: tg.MemberMember})
	}
	r := listChats(t, h, tok)
	if len(r.Chats) != 1+listCallsPerMin || !r.Partial {
		t.Fatalf("%d chats, partial %v; want %d chats", len(r.Chats), r.Partial, 1+listCallsPerMin)
	}
	if c := len(h.members.Calls()); c != listCallsPerMin {
		t.Fatalf("%d calls, want %d", c, listCallsPerMin)
	}
	// The actor budget is untouched.
	addGroup(t, h, grp, "Family")
	h.members.Set(grp, idAnn, tg.ChatMember{Status: tg.MemberMember})
	if err := h.actor(grp); err != nil {
		t.Fatalf("groupActor after a list: %v", err)
	}
}

// After the stop of the list, no new call starts.
func TestNoNewCallAfterTheListStopped(t *testing.T) {
	h := newHarness(t)
	addGroup(t, h, grp, "Family")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.srv.askTelegram(context.Background(), ctx, h.srv.members.listCalls, idAnn, grp); !errors.Is(err, errTemp) {
		t.Fatalf("err %v", err)
	}
	if n := len(h.members.Calls()); n != 0 {
		t.Fatalf("%d calls", n)
	}
}
