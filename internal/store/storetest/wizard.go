package storetest

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Wizard drafts (#68, ARCHITECTURE §12).

// draft returns a draft of chat -5 by user 7 on card msg, live until base+1h.
func draft(msg int) model.Draft {
	due := base.Add(24 * time.Hour)
	return model.Draft{
		ChatID: -5, UserID: 7, MsgID: msg, Step: model.StepDate,
		Data: model.DraftData{
			Text: "buy milk", Date: "2026-03-02", Time: "15:00", Repeat: model.RepeatWeekly,
			AssigneeID: 9, FoundText: "buy milk", FoundDue: &due, FoundRule: "weekly mon 15:00",
			FoundTime: true, Editing: true,
		},
		UpdatedAt: base, ExpiresAt: *tp(1),
	}
}

func testDrafts(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	now := base.Add(time.Minute)

	if _, err := a.GetDraft(ctx, -5, 7, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetDraft before Put: %v, want ErrNotFound", err)
	}
	d := draft(40)
	if err := a.PutDraft(ctx, &d); err != nil {
		t.Fatal(err)
	}
	if d.Version == 0 {
		t.Fatal("PutDraft left Version 0")
	}
	got, err := b.GetDraft(ctx, -5, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, d)
	}
	if got, err = b.DraftByMessage(ctx, -5, 40, now); err != nil || got.UserID != 7 {
		t.Fatalf("DraftByMessage = %+v, %v", got, err)
	}
	for _, c := range []struct {
		chat int64
		msg  int
	}{{-5, 41}, {-6, 40}, {-5, 0}} {
		if _, err := b.DraftByMessage(ctx, c.chat, c.msg, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("DraftByMessage(%d, %d): %v, want ErrNotFound", c.chat, c.msg, err)
		}
	}

	// UpdateDraft with the current version wins and bumps it; the old
	// version then loses and writes nothing.
	old := d.Version
	d.Step, d.Data = model.StepTime, model.DraftData{Text: "buy bread"}
	d.UpdatedAt, d.ExpiresAt = now, now.Add(30*time.Minute)
	if ok, err := b.UpdateDraft(ctx, &d, now); err != nil || !ok {
		t.Fatalf("UpdateDraft = %v, %v", ok, err)
	}
	if d.Version <= old {
		t.Fatalf("version %d after update, want > %d", d.Version, old)
	}
	stale := d
	stale.Version, stale.Step = old, model.StepConfirm
	if ok, err := a.UpdateDraft(ctx, &stale, now); err != nil || ok {
		t.Fatalf("stale UpdateDraft = %v, %v; want false", ok, err)
	}
	if stale.Version != old {
		t.Fatal("a failed UpdateDraft changed Version")
	}
	if got, _ = a.GetDraft(ctx, -5, 7, now); !reflect.DeepEqual(got, d) {
		t.Fatalf("after updates:\n got %+v\nwant %+v", got, d)
	}

	// PutDraft replaces, with a greater version, whatever is stored.
	r := draft(50)
	if err := a.PutDraft(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if r.Version <= d.Version {
		t.Fatalf("replacement version %d, want > %d", r.Version, d.Version)
	}
	if _, err := a.DraftByMessage(ctx, -5, 40, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced card still found: %v", err)
	}
	if ok, _ := a.UpdateDraft(ctx, &d, now); ok {
		t.Fatal("UpdateDraft with the replaced draft's version succeeded")
	}

	// Another user's draft in the same chat is independent.
	o := draft(60)
	o.UserID = 8
	if err := b.PutDraft(ctx, &o); err != nil {
		t.Fatal(err)
	}

	// DeleteDraft: wrong version loses, current one wins once.
	if ok, err := a.DeleteDraft(ctx, -5, 7, r.Version+1); err != nil || ok {
		t.Fatalf("DeleteDraft(wrong version) = %v, %v", ok, err)
	}
	if ok, err := a.DeleteDraft(ctx, -5, 7, r.Version); err != nil || !ok {
		t.Fatalf("DeleteDraft = %v, %v", ok, err)
	}
	if ok, err := b.DeleteDraft(ctx, -5, 7, r.Version); err != nil || ok {
		t.Fatalf("second DeleteDraft = %v, %v", ok, err)
	}
	if _, err := a.GetDraft(ctx, -5, 7, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted draft found: %v", err)
	}
	if _, err := a.GetDraft(ctx, -5, 8, now); err != nil {
		t.Fatalf("other user's draft: %v", err)
	}
}

// An expired draft is missing for reads and conditional writes, can be
// replaced, and is removed by Purge.
func testDraftExpiry(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a := open()
	d := draft(40) // expires at base+1h
	if err := a.PutDraft(ctx, &d); err != nil {
		t.Fatal(err)
	}
	live, at := *tp(1), *tp(1)
	live = live.Add(-time.Second)
	if _, err := a.GetDraft(ctx, -5, 7, live); err != nil {
		t.Fatalf("live draft: %v", err)
	}
	if _, err := a.GetDraft(ctx, -5, 7, at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetDraft at expiry: %v, want ErrNotFound", err)
	}
	if _, err := a.DraftByMessage(ctx, -5, 40, at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DraftByMessage at expiry: %v, want ErrNotFound", err)
	}
	upd := d
	upd.ExpiresAt = at.Add(30 * time.Minute)
	if ok, err := a.UpdateDraft(ctx, &upd, at); err != nil || ok {
		t.Fatalf("UpdateDraft of an expired draft = %v, %v; want false", ok, err)
	}

	// Purge removes expired drafts only.
	o := draft(60)
	o.UserID, o.ExpiresAt = 8, *tp(3)
	if err := a.PutDraft(ctx, &o); err != nil {
		t.Fatal(err)
	}
	if err := a.Purge(ctx, at); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.DeleteDraft(ctx, -5, 7, d.Version); err != nil || ok {
		t.Fatalf("expired draft survived Purge: ok=%v err=%v", ok, err)
	}
	if _, err := a.GetDraft(ctx, -5, 8, at); err != nil {
		t.Fatalf("live draft purged: %v", err)
	}

	// Starting again over an expired draft works.
	n := draft(70)
	n.UpdatedAt, n.ExpiresAt = at, at.Add(30*time.Minute)
	o.ExpiresAt = at // expire user 8's draft, then replace it
	if ok, err := a.UpdateDraft(ctx, &o, at.Add(-time.Second)); err != nil || !ok {
		t.Fatalf("UpdateDraft = %v, %v", ok, err)
	}
	n.UserID = 8
	if err := a.PutDraft(ctx, &n); err != nil {
		t.Fatal(err)
	}
	if got, err := a.GetDraft(ctx, -5, 8, at); err != nil || got.MsgID != 70 {
		t.Fatalf("replaced expired draft = %+v, %v", got, err)
	}
}

// Several nodes pressing buttons of one card with the same version: exactly
// one UpdateDraft wins, and exactly one DeleteDraft wins.
func testDraftConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	now := base.Add(time.Minute)
	d := draft(40)
	if err := hs[0].PutDraft(ctx, &d); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	wins := 0
	parallel(8, func(i int) {
		c := d
		c.Data.Text = "press"
		ok, err := hs[i%len(hs)].UpdateDraft(ctx, &c, now)
		if err != nil {
			t.Error(err)
		}
		if ok {
			mu.Lock()
			wins++
			mu.Unlock()
		}
	})
	if wins != 1 {
		t.Fatalf("%d UpdateDraft winners, want 1", wins)
	}
	cur, err := hs[0].GetDraft(ctx, -5, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	wins = 0
	parallel(8, func(i int) {
		ok, err := hs[i%len(hs)].DeleteDraft(ctx, -5, 7, cur.Version)
		if err != nil {
			t.Error(err)
		}
		if ok {
			mu.Lock()
			wins++
			mu.Unlock()
		}
	})
	if wins != 1 {
		t.Fatalf("%d DeleteDraft winners, want 1", wins)
	}
}

// Concurrent starts of one (chat, user) never fail and never hand out the
// same version twice, so a stale card can never match a replacement.
func testPutDraftConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	var mu sync.Mutex
	seen := map[int64]int{}
	parallel(8, func(i int) {
		d := draft(100 + i)
		if err := hs[i%len(hs)].PutDraft(ctx, &d); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		seen[d.Version]++
		mu.Unlock()
	})
	if len(seen) != 8 {
		t.Fatalf("versions handed out: %v, want 8 distinct", seen)
	}
}

func testListNamedUsers(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	if us, err := d.ListNamedUsers(ctx); err != nil || len(us) != 0 {
		t.Fatalf("empty: %v, %v", us, err)
	}
	for _, id := range []int64{1, 2, 3} {
		user(t, d, id)
	}
	setName(t, d, 1, "Zed")
	setName(t, d, 3, "amy")
	us, err := d.ListNamedUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range us {
		got = append(got, u.Username)
	}
	if !reflect.DeepEqual(got, []string{"amy", "zed"}) || us[0].ID != 3 || us[1].ID != 1 {
		t.Fatalf("ListNamedUsers = %+v", us)
	}
}
