package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// A stale renewal by the holder (older now, landing late) must not move
// held_until backwards, and must not change the epoch.
func testLeaseStaleRenewal(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	const ttl = 30 * time.Second
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }

	l, err := d.AcquireLease(ctx, "poller", "A", at(30), ttl)
	if err != nil || l.Epoch != 1 || !l.Until.Equal(at(60)) {
		t.Fatalf("renew now=30: %+v, %v", l, err)
	}
	l, err = d.AcquireLease(ctx, "poller", "A", at(10), ttl)
	if err != nil || l.Holder != "A" || l.Epoch != 1 || !l.Until.Equal(at(60)) {
		t.Fatalf("stale renew now=10: %+v, %v (held_until must stay 60)", l, err)
	}
	// A peer still cannot take it before the later expiry.
	if l, _ = d.AcquireLease(ctx, "poller", "B", at(45), ttl); l.Holder != "A" {
		t.Fatalf("peer took the lease at 45: %+v", l)
	}
	// A normal later renewal still extends it.
	if l, _ = d.AcquireLease(ctx, "poller", "A", at(50), ttl); !l.Until.Equal(at(80)) || l.Epoch != 1 {
		t.Fatalf("renew now=50: %+v", l)
	}
}

// The digest claim survives MigrateChat into an existing chat.
func testMigrateChatDigestClaim(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	digestChat(t, d, 1, *tp(1))
	chat(t, d, -100) // target exists, no digest
	now := *tp(3)
	if got, err := d.ClaimDigests(ctx, "tok", now, now.Add(claimTTL), 10); err != nil || len(got) != 1 {
		t.Fatalf("claim = %v, %v", chatIDs(got), err)
	}
	if err := d.MigrateChat(ctx, 1, -100); err != nil {
		t.Fatal(err)
	}
	c, err := d.GetChat(ctx, -100)
	if err != nil || c.DigestTime != "08:00" || c.DigestBy != 7 || c.DigestNextAt == nil ||
		!c.DigestNextAt.Equal(*tp(1)) || c.DigestClaimToken != "tok" {
		t.Fatalf("merged chat = %+v, %v", c, err)
	}
	// The claim is still live: other nodes cannot claim it.
	if got, _ := d.ClaimDigests(ctx, "other", now, now.Add(claimTTL), 10); len(got) != 0 {
		t.Fatalf("claimed a live claim: %v", chatIDs(got))
	}
	if ok, err := d.AdvanceDigest(ctx, -100, "tok", *tp(30)); err != nil || !ok {
		t.Fatalf("AdvanceDigest = %v, %v; want ok", ok, err)
	}
	if _, err := d.GetChat(ctx, 1); err == nil {
		t.Fatal("old chat still exists")
	}
}

// Precedence when both chats have digest state: the target's wins as a whole.
func testMigrateChatDigestPrecedence(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	digestChat(t, d, 1, *tp(1))
	digestChat(t, d, -100, *tp(2))
	if err := d.SetDigest(ctx, -100, "09:30", 8, tp(2)); err != nil {
		t.Fatal(err)
	}
	now := *tp(3)
	if got, _ := d.ClaimDigests(ctx, "tok", now, now.Add(claimTTL), 10); len(got) != 2 {
		t.Fatalf("claim = %v", chatIDs(got))
	}
	if err := d.MigrateChat(ctx, 1, -100); err != nil {
		t.Fatal(err)
	}
	c, _ := d.GetChat(ctx, -100)
	if c.DigestTime != "09:30" || c.DigestBy != 8 || !c.DigestNextAt.Equal(*tp(2)) || c.DigestClaimToken != "tok" {
		t.Fatalf("target digest state changed: %+v", c)
	}
	// Neither chat has a digest: nothing appears.
	chat(t, d, 5)
	chat(t, d, 6)
	if err := d.MigrateChat(ctx, 5, 6); err != nil {
		t.Fatal(err)
	}
	if c, _ = d.GetChat(ctx, 6); c.DigestTime != "" || c.DigestNextAt != nil || c.DigestClaimToken != "" {
		t.Fatalf("digest appeared: %+v", c)
	}
}
