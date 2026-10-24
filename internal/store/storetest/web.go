package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Web UI (#102, docs/WEB-DESIGN.md §6.3, §6.4).

func webSession(id string, user int64, kind model.WebSessionKind, created time.Time) model.WebSession {
	return model.WebSession{
		ID: id, UserID: user, Kind: kind, Device: "dev " + id,
		CreatedAt: created, SeenAt: created,
		ExpiresAt: created.Add(12 * time.Hour), MaxAt: created.Add(24 * time.Hour),
	}
}

func putSession(t *testing.T, d store.Store, s model.WebSession) {
	t.Helper()
	if err := d.PutWebSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

// sessionIDs returns which of ids are live at base.
func sessionIDs(t *testing.T, d store.Store, ids ...string) []string {
	t.Helper()
	var out []string
	for _, id := range ids {
		_, err := d.GetWebSession(context.Background(), id, base)
		switch {
		case err == nil:
			out = append(out, id)
		case !errors.Is(err, store.ErrNotFound):
			t.Fatal(err)
		}
	}
	return out
}

func testWebSessions(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	s := webSession("aa", 7, model.WebBrowser, base)
	putSession(t, d, s)

	got, err := d.GetWebSession(ctx, "aa", base)
	if err != nil {
		t.Fatal(err)
	}
	if got != s {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, s)
	}
	if _, err := d.GetWebSession(ctx, "nope", base); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing session: %v", err)
	}
	// Expiry is "<= now", for expires_at and for max_at.
	if _, err := d.GetWebSession(ctx, "aa", s.ExpiresAt.Add(-time.Second)); err != nil {
		t.Fatalf("live before expires_at: %v", err)
	}
	if _, err := d.GetWebSession(ctx, "aa", s.ExpiresAt); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("at expires_at: %v", err)
	}
	m := webSession("mx", 7, model.WebBrowser, base)
	m.ExpiresAt, m.MaxAt = base.Add(48*time.Hour), base.Add(time.Hour)
	putSession(t, d, m)
	if _, err := d.GetWebSession(ctx, "mx", base.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("at max_at: %v", err)
	}

	// Touch writes seen_at and expires_at only.
	seen, exp := base.Add(2*time.Hour), base.Add(14*time.Hour)
	if err := d.TouchWebSession(ctx, "aa", seen, exp); err != nil {
		t.Fatal(err)
	}
	got, err = d.GetWebSession(ctx, "aa", base)
	if err != nil {
		t.Fatal(err)
	}
	want := s
	want.SeenAt, want.ExpiresAt = seen, exp
	if got != want {
		t.Fatalf("after touch:\n got %+v\nwant %+v", got, want)
	}
	if err := d.TouchWebSession(ctx, "nope", seen, exp); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("touch missing: %v", err)
	}

	// Deletes.
	putSession(t, d, webSession("bb", 7, model.WebMiniApp, base))
	putSession(t, d, webSession("cc", 8, model.WebBrowser, base))
	if err := d.DeleteWebSession(ctx, "aa"); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteWebSession(ctx, "aa"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if got := sessionIDs(t, d, "aa", "bb", "cc"); fmt.Sprint(got) != "[bb cc]" {
		t.Fatalf("after delete: %v", got)
	}
	if err := d.DeleteUserWebSessions(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if got := sessionIDs(t, d, "bb", "cc", "mx"); fmt.Sprint(got) != "[cc]" {
		t.Fatalf("after delete of user 7: %v", got)
	}
}

func testTrimWebSessions(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	var browser, mini []string
	for i := range 4 {
		b := fmt.Sprintf("b%d", i)
		m := fmt.Sprintf("m%d", i)
		putSession(t, d, webSession(b, 7, model.WebBrowser, base.Add(time.Duration(i)*time.Minute)))
		putSession(t, d, webSession(m, 7, model.WebMiniApp, base.Add(time.Duration(i)*time.Minute)))
		browser, mini = append(browser, b), append(mini, m)
	}
	putSession(t, d, webSession("other", 8, model.WebMiniApp, base))

	if err := d.TrimUserWebSessions(ctx, 7, model.WebMiniApp, 2); err != nil {
		t.Fatal(err)
	}
	if got := sessionIDs(t, d, mini...); fmt.Sprint(got) != "[m2 m3]" {
		t.Fatalf("miniapp after trim to 2: %v", got)
	}
	if got := sessionIDs(t, d, browser...); len(got) != 4 {
		t.Fatalf("trim of miniapp touched browser sessions: %v", got)
	}
	if got := sessionIDs(t, d, "other"); len(got) != 1 {
		t.Fatal("trim touched another user")
	}
	if err := d.TrimUserWebSessions(ctx, 7, model.WebBrowser, 10); err != nil {
		t.Fatal(err)
	}
	if got := sessionIDs(t, d, browser...); len(got) != 4 {
		t.Fatalf("trim above the count deleted sessions: %v", got)
	}
	if err := d.TrimUserWebSessions(ctx, 7, model.WebBrowser, 0); err != nil {
		t.Fatal(err)
	}
	if got := sessionIDs(t, d, browser...); len(got) != 0 {
		t.Fatalf("trim to 0 kept %v", got)
	}
}

func testDeleteWebSessionsExcept(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	putSession(t, d, webSession("a1", 1, model.WebBrowser, base))
	putSession(t, d, webSession("a2", 1, model.WebMiniApp, base))
	putSession(t, d, webSession("b1", 2, model.WebBrowser, base))
	putSession(t, d, webSession("c1", 3, model.WebBrowser, base))

	n, err := d.DeleteWebSessionsExcept(ctx, []int64{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d, want 1", n)
	}
	if got := sessionIDs(t, d, "a1", "a2", "b1", "c1"); fmt.Sprint(got) != "[a1 a2 c1]" {
		t.Fatalf("after except {1,3}: %v", got)
	}
	n, err = d.DeleteWebSessionsExcept(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("empty keep deleted %d, want 3", n)
	}
}

func code(hash string, user int64, created time.Time) model.LoginCode {
	return model.LoginCode{CodeHash: hash, UserID: user, CreatedAt: created, ExpiresAt: created.Add(5 * time.Minute)}
}

func putCode(t *testing.T, d store.Store, c model.LoginCode, maxLive int, now time.Time) bool {
	t.Helper()
	ok, err := d.PutLoginCode(context.Background(), c, maxLive, now)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func testLoginCodes(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	c1 := code("h1", 7, base)
	if !putCode(t, d, c1, 100, base) {
		t.Fatal("first code refused")
	}
	// A second code of the same user replaces the first one.
	c2 := code("h2", 7, base.Add(time.Minute))
	if !putCode(t, d, c2, 100, base.Add(time.Minute)) {
		t.Fatal("second code refused")
	}
	if _, err := d.TakeLoginCode(ctx, "h1", base.Add(time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old code still live: %v", err)
	}
	got, err := d.TakeLoginCode(ctx, "h2", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got != c2 {
		t.Fatalf("take:\n got %+v\nwant %+v", got, c2)
	}
	// A code works one time.
	if _, err := d.TakeLoginCode(ctx, "h2", base.Add(time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second take: %v", err)
	}
	// An expired code cannot be taken ("<= now").
	c3 := code("h3", 8, base)
	putCode(t, d, c3, 100, base)
	if _, err := d.TakeLoginCode(ctx, "h3", c3.ExpiresAt); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("take at expiry: %v", err)
	}
	if _, err := d.TakeLoginCode(ctx, "h3", c3.ExpiresAt.Add(-time.Second)); err != nil {
		t.Fatalf("take before expiry: %v", err)
	}

	// DeleteUserLoginCodes.
	putCode(t, d, code("h4", 9, base), 100, base)
	putCode(t, d, code("h5", 10, base), 100, base)
	if err := d.DeleteUserLoginCodes(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TakeLoginCode(ctx, "h4", base); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("code of user 9 kept: %v", err)
	}
	if _, err := d.TakeLoginCode(ctx, "h5", base); err != nil {
		t.Fatalf("code of user 10 lost: %v", err)
	}
}

func testLoginCodeCap(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	const maxLive = 3
	for i := range maxLive {
		if !putCode(t, d, code(fmt.Sprintf("u%d", i), int64(100+i), base), maxLive, base) {
			t.Fatalf("code %d refused below the cap", i)
		}
	}
	// The cap refuses a new user, and inserts nothing.
	if putCode(t, d, code("new", 200, base), maxLive, base) {
		t.Fatal("cap not applied")
	}
	if _, err := d.TakeLoginCode(ctx, "new", base); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refused code was stored: %v", err)
	}
	// A user at the cap can replace their own code.
	if !putCode(t, d, code("u0b", 100, base), maxLive, base) {
		t.Fatal("own replacement refused at the cap")
	}
	if _, err := d.TakeLoginCode(ctx, "u0", base); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced code still live: %v", err)
	}
	// Expired codes do not count.
	later := base.Add(5 * time.Minute)
	if !putCode(t, d, code("late", 200, later), maxLive, later) {
		t.Fatal("expired codes counted against the cap")
	}
	// A refused put keeps the user's old code.
	for i := range 2 {
		putCode(t, d, code(fmt.Sprintf("l%d", i), int64(300+i), later), maxLive, later)
	}
	if putCode(t, d, code("x", 400, later), maxLive, later) {
		t.Fatal("cap not applied after expiry")
	}
	putCode(t, d, code("y", 300, later), 1, later) // refused: two other users are live
	if _, err := d.TakeLoginCode(ctx, "l0", later); err != nil {
		t.Fatalf("refused put deleted the old code: %v", err)
	}
}

func testTakeLoginCodeConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	putCode(t, a, code("race", 7, base), 100, base)
	var wins, errs atomic.Int32
	parallel(8, func(i int) {
		d := a
		if i%2 == 1 {
			d = b
		}
		_, err := d.TakeLoginCode(ctx, "race", base)
		switch {
		case err == nil:
			wins.Add(1)
		case !errors.Is(err, store.ErrNotFound):
			errs.Add(1)
			t.Error(err)
		}
	})
	if wins.Load() != 1 || errs.Load() != 0 {
		t.Fatalf("wins = %d, errors = %d; want exactly one winner", wins.Load(), errs.Load())
	}
}

func testUseProof(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	exp := base.Add(10 * time.Minute)
	fresh, err := d.UseProof(ctx, "p", exp, base)
	if err != nil || !fresh {
		t.Fatalf("first use: %v, %v", fresh, err)
	}
	fresh, err = d.UseProof(ctx, "p", exp, base.Add(time.Minute))
	if err != nil || fresh {
		t.Fatalf("second use: %v, %v", fresh, err)
	}
	// After its expiry the old row does not block the hash.
	fresh, err = d.UseProof(ctx, "p", exp.Add(10*time.Minute), exp)
	if err != nil || !fresh {
		t.Fatalf("use after expiry: %v, %v", fresh, err)
	}
	fresh, err = d.UseProof(ctx, "p", exp.Add(10*time.Minute), exp.Add(time.Second))
	if err != nil || fresh {
		t.Fatalf("reuse of the new row: %v, %v", fresh, err)
	}
}

func testUseProofConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	var wins, errs atomic.Int32
	parallel(8, func(i int) {
		d := a
		if i%2 == 1 {
			d = b
		}
		fresh, err := d.UseProof(ctx, "race", base.Add(10*time.Minute), base)
		if err != nil {
			errs.Add(1)
			t.Error(err)
		} else if fresh {
			wins.Add(1)
		}
	})
	if wins.Load() != 1 || errs.Load() != 0 {
		t.Fatalf("wins = %d, errors = %d; want exactly one winner", wins.Load(), errs.Load())
	}
}

func getMember(t *testing.T, d store.Store, chatID, userID int64) model.ChatMember {
	t.Helper()
	m, err := d.GetChatMember(context.Background(), chatID, userID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func testChatMembers(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	if _, err := d.GetChatMember(ctx, -1, 7); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing row: %v", err)
	}

	// A hint inserts 'unknown' with seen_at and no check.
	if err := d.NoteChatMember(ctx, -1, 7, base); err != nil {
		t.Fatal(err)
	}
	m := getMember(t, d, -1, 7)
	want := model.ChatMember{ChatID: -1, UserID: 7, Status: model.MemberUnknown, SeenAt: base}
	if m != want || !m.CheckedAt.IsZero() {
		t.Fatalf("hint row:\n got %+v\nwant %+v", m, want)
	}

	// A check writes status and checked_at and keeps seen_at.
	chk := base.Add(time.Minute)
	if err := d.SetChatMember(ctx, model.ChatMember{ChatID: -1, UserID: 7, Status: model.MemberYes,
		SeenAt: base.Add(time.Hour), CheckedAt: chk}); err != nil {
		t.Fatal(err)
	}
	want = model.ChatMember{ChatID: -1, UserID: 7, Status: model.MemberYes, SeenAt: base, CheckedAt: chk}
	if m := getMember(t, d, -1, 7); m != want {
		t.Fatalf("after check:\n got %+v\nwant %+v", m, want)
	}

	// A later hint changes only seen_at.
	seen := base.Add(2 * time.Hour)
	if err := d.NoteChatMember(ctx, -1, 7, seen); err != nil {
		t.Fatal(err)
	}
	want.SeenAt = seen
	if m := getMember(t, d, -1, 7); m != want {
		t.Fatalf("after second hint:\n got %+v\nwant %+v", m, want)
	}

	// A check of a new pair inserts it, with no seen_at.
	if err := d.SetChatMember(ctx, model.ChatMember{ChatID: -2, UserID: 7, Status: model.MemberLeft,
		CheckedAt: chk}); err != nil {
		t.Fatal(err)
	}
	if m := getMember(t, d, -2, 7); m.Status != model.MemberLeft || !m.SeenAt.IsZero() || m.CheckedAt != chk {
		t.Fatalf("new checked row: %+v", m)
	}

	// ListChatMembersOf: every row of the user, by chat_id.
	if err := d.NoteChatMember(ctx, -3, 7, base); err != nil {
		t.Fatal(err)
	}
	if err := d.NoteChatMember(ctx, -1, 8, base); err != nil {
		t.Fatal(err)
	}
	rows, err := d.ListChatMembersOf(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	var chats []int64
	for _, r := range rows {
		if r.UserID != 7 {
			t.Fatalf("row of another user: %+v", r)
		}
		chats = append(chats, r.ChatID)
	}
	if fmt.Sprint(chats) != "[-3 -2 -1]" {
		t.Fatalf("chats = %v", chats)
	}
	if rows, err := d.ListChatMembersOf(ctx, 99); err != nil || len(rows) != 0 {
		t.Fatalf("no rows: %v, %v", rows, err)
	}

	// SetChatGone: every row of the chat becomes 'left', and seen_at stays.
	gone := base.Add(3 * time.Hour)
	if err := d.SetChatGone(ctx, -1, gone); err != nil {
		t.Fatal(err)
	}
	for _, u := range []int64{7, 8} {
		m := getMember(t, d, -1, u)
		if m.Status != model.MemberLeft || m.CheckedAt != gone || m.SeenAt.IsZero() {
			t.Fatalf("user %d after gone: %+v", u, m)
		}
	}
	if m := getMember(t, d, -3, 7); m.Status != model.MemberUnknown {
		t.Fatalf("gone touched another chat: %+v", m)
	}
}

func testListGroupChats(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	for _, c := range []model.Chat{
		{ID: -30, Kind: model.ChatGroup, Title: "c"},
		{ID: 7, Kind: model.ChatPrivate},
		{ID: -10, Kind: model.ChatGroup, Title: "a"},
		{ID: -20, Kind: model.ChatGroup, Title: "b"},
	} {
		c.CreatedAt = base
		if err := d.EnsureChat(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	list := func(limit int) string {
		cs, err := d.ListGroupChats(ctx, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cs {
			if c.Kind != model.ChatGroup {
				t.Fatalf("not a group: %+v", c)
			}
			out = append(out, fmt.Sprintf("%d:%s", c.ID, c.Title))
		}
		return fmt.Sprint(out)
	}
	if got := list(50); got != "[-30:c -20:b -10:a]" {
		t.Fatalf("list = %s", got)
	}
	if got := list(2); got != "[-30:c -20:b]" {
		t.Fatalf("limit 2 = %s", got)
	}
	if got := list(0); got != "[]" {
		t.Fatalf("limit 0 = %s", got)
	}
}

func testWebPurge(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	now := base.Add(100 * 24 * time.Hour)

	live := webSession("live", 7, model.WebBrowser, now)
	idle := webSession("idle", 7, model.WebBrowser, base)
	idle.ExpiresAt, idle.MaxAt = now, now.Add(time.Hour) // expires_at <= now
	capped := webSession("capped", 7, model.WebBrowser, base)
	capped.ExpiresAt, capped.MaxAt = now.Add(time.Hour), now // max_at <= now
	for _, s := range []model.WebSession{live, idle, capped} {
		putSession(t, d, s)
	}
	putCode(t, d, code("old", 7, now.Add(-5*time.Minute)), 100, now.Add(-5*time.Minute)) // expires at now
	putCode(t, d, code("new", 8, now), 100, now)
	if _, err := d.UseProof(ctx, "oldp", now, base); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UseProof(ctx, "newp", now.Add(time.Minute), base); err != nil {
		t.Fatal(err)
	}

	month := 30 * 24 * time.Hour
	old := now.Add(-month - time.Second)
	recent := now.Add(-month + time.Second)
	set := func(chat int64, st model.MemberStatus, seen, checked time.Time) {
		t.Helper()
		if !seen.IsZero() {
			if err := d.NoteChatMember(ctx, chat, 7, seen); err != nil {
				t.Fatal(err)
			}
		}
		if st != model.MemberUnknown {
			if err := d.SetChatMember(ctx, model.ChatMember{ChatID: chat, UserID: 7, Status: st, CheckedAt: checked}); err != nil {
				t.Fatal(err)
			}
		}
	}
	set(-1, model.MemberYes, old, old)             // member: kept
	set(-2, model.MemberLeft, old, old)            // old left: purged
	set(-3, model.MemberUnknown, old, time.Time{}) // old hint, never checked: purged
	set(-4, model.MemberLeft, recent, old)         // recent hint: kept
	set(-5, model.MemberLeft, old, recent)         // recent check: kept
	set(-6, model.MemberUnknown, recent, time.Time{})

	if err := d.Purge(ctx, now); err != nil {
		t.Fatal(err)
	}

	// Read the sessions at a time when all three are otherwise live, so
	// that only a deleted row is missing.
	for id, wantLive := range map[string]bool{"live": true, "idle": false, "capped": false} {
		_, err := d.GetWebSession(ctx, id, base)
		if gotLive := err == nil; gotLive != wantLive {
			t.Errorf("session %s: live=%v after purge, want %v (%v)", id, gotLive, wantLive, err)
		}
	}
	if _, err := d.TakeLoginCode(ctx, "old", base); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expired code kept: %v", err)
	}
	if _, err := d.TakeLoginCode(ctx, "new", now); err != nil {
		t.Errorf("live code purged: %v", err)
	}
	// A purged proof row lets the hash in again at a time before its old
	// expiry; a kept one does not.
	if fresh, err := d.UseProof(ctx, "oldp", now.Add(time.Hour), base); err != nil || !fresh {
		t.Errorf("expired proof kept: %v, %v", fresh, err)
	}
	if fresh, err := d.UseProof(ctx, "newp", now.Add(time.Hour), now); err != nil || fresh {
		t.Errorf("live proof purged: %v, %v", fresh, err)
	}
	for chat, wantKept := range map[int64]bool{-1: true, -2: false, -3: false, -4: true, -5: true, -6: true} {
		_, err := d.GetChatMember(ctx, chat, 7)
		if kept := err == nil; kept != wantKept {
			t.Errorf("chat_members %d: kept=%v, want %v (%v)", chat, kept, wantKept, err)
		}
	}
}
