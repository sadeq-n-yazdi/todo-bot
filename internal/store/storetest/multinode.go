package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

const claimTTL = 2 * time.Minute

// parallel runs fn(i) for i in [0,n) at the same time and waits.
func parallel(n int, fn func(i int)) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

func ids(drs []model.DueReminder) []int64 {
	out := make([]int64, len(drs))
	for i, d := range drs {
		out[i] = d.ID
	}
	return out
}

// ---- reminder claims ----

func testClaimReminders(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	r1 := reminder(t, d, 1, model.ReminderDue)
	r2 := reminder(t, d, 2, model.ReminderCustom)
	r3 := reminder(t, d, 3, model.ReminderDue)
	future := reminder(t, d, 10, model.ReminderSnooze)
	cancelled := reminder(t, d, 1, model.ReminderCustom)
	cancelled.State = model.ReminderCancelled
	if err := d.UpdateReminder(ctx, cancelled); err != nil {
		t.Fatal(err)
	}

	now := *tp(3)
	got, err := d.ClaimReminders(ctx, "a", now, now.Add(claimTTL), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != r1.ID || got[1].ID != r2.ID {
		t.Fatalf("limit/order wrong: %v (want %d, %d)", ids(got), r1.ID, r2.ID)
	}
	for _, g := range got {
		if g.ClaimToken != "a" || g.Todo.ID != g.TodoID || g.Todo.Text != "t" || g.State != model.ReminderPending {
			t.Fatalf("claimed reminder wrong: %+v", g)
		}
	}
	// Claimed rows are invisible until claim_until; the rest is still claimable.
	got, _ = d.ClaimReminders(ctx, "b", now, now.Add(claimTTL), 10)
	if len(got) != 1 || got[0].ID != r3.ID {
		t.Fatalf("second claim = %v, want only %d", ids(got), r3.ID)
	}
	if got, _ = d.ClaimReminders(ctx, "c", now.Add(claimTTL-time.Second), now.Add(2*claimTTL), 10); len(got) != 0 {
		t.Fatalf("claims visible before expiry: %v", ids(got))
	}
	// Expiry is "<= now": claim_until itself is claimable again.
	got, _ = d.ClaimReminders(ctx, "d", now.Add(claimTTL), now.Add(2*claimTTL), 10)
	if len(got) != 3 || got[0].ClaimToken != "d" {
		t.Fatalf("expired claims not retaken: %v", ids(got))
	}
	// Not-yet-due and non-pending rows were never claimed.
	if g := getReminder(t, d, future.ID); g.ClaimToken != "" || g.State != model.ReminderPending {
		t.Fatalf("future reminder touched: %+v", g)
	}
	if got, err := d.ClaimReminders(ctx, "e", now, now.Add(claimTTL), 0); err != nil || len(got) != 0 {
		t.Fatalf("limit 0 = %v, %v", got, err)
	}
	if _, err := d.ClaimReminders(ctx, "", now, now.Add(claimTTL), 5); err == nil {
		t.Fatal("empty token must be an error")
	}
	if _, err := d.ClaimReminders(ctx, "", now, now.Add(claimTTL), 0); err == nil {
		t.Fatal("empty token must be an error, also with limit 0")
	}
}

// UpdateReminder (a user action) clears a claim: the old claimer loses and
// the reminder can be claimed at once.
func testUpdateReminderClearsClaim(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	r := reminder(t, a, 1, model.ReminderDue)
	now := *tp(2)
	ra := again0(t, a, "A", now)
	if g := getReminder(t, b, r.ID); g.ClaimToken != "A" {
		t.Fatalf("claim token not stored: %+v", g)
	}
	snoozed := getReminder(t, b, r.ID)
	snoozed.FireAt = now.Add(time.Minute)
	if err := b.UpdateReminder(ctx, snoozed); err != nil {
		t.Fatal(err)
	}
	if g := getReminder(t, b, r.ID); g.ClaimToken != "" {
		t.Fatalf("claim survived UpdateReminder: %+v", g)
	}
	ra.FireAt = now.Add(time.Hour)
	if ok, err := a.RescheduleClaimed(ctx, ra); err != nil || ok {
		t.Fatalf("old claimer: ok=%v err=%v", ok, err)
	}
	if g := getReminder(t, b, r.ID); !g.FireAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("old claimer overwrote the update: %+v", g)
	}
	// Claimable immediately, long before the old claim_until.
	if got, _ := b.ClaimReminders(ctx, "B", now.Add(time.Minute), now.Add(claimTTL), 10); len(got) != 1 {
		t.Fatalf("not claimable after UpdateReminder: %v", ids(got))
	}
}

// claimAll drains claims through 3 handles x 4 goroutines in batches of 7
// (the shape #26 specifies) and checks that every id was claimed exactly once.
// claim must use the token it is given, which is unique per call.
func claimAll(t *testing.T, hs []store.Store, want int, claim func(h store.Store, token string, limit int) ([]int64, error)) {
	t.Helper()
	var mu sync.Mutex
	seen := map[int64]string{}
	var calls atomic.Int64
	parallel(12, func(i int) {
		h := hs[i/4]
		for {
			token := fmt.Sprintf("tok-%d", calls.Add(1))
			got, err := claim(h, token, 7)
			if err != nil {
				t.Error(err)
				return
			}
			if len(got) == 0 {
				return
			}
			if len(got) > 7 {
				t.Errorf("batch of %d exceeds the limit", len(got))
			}
			mu.Lock()
			for _, id := range got {
				if prev, dup := seen[id]; dup {
					t.Errorf("id %d claimed by %s and %s", id, prev, token)
				}
				seen[id] = token
			}
			mu.Unlock()
		}
	})
	if len(seen) != want {
		t.Fatalf("claimed %d of %d", len(seen), want)
	}
}

func testClaimRemindersConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 3)
	const n = 100
	for i := range n {
		reminder(t, hs[0], i%3, model.ReminderDue)
	}
	now := *tp(5)
	claimAll(t, hs, n, func(h store.Store, token string, limit int) ([]int64, error) {
		got, err := h.ClaimReminders(ctx, token, now, now.Add(claimTTL), limit)
		for _, g := range got {
			if g.ClaimToken != token {
				t.Errorf("token = %q, want %q", g.ClaimToken, token)
			}
		}
		return ids(got), err
	})
}

func testRescheduleClaimed(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	r := reminder(t, d, 1, model.ReminderDue)
	now := *tp(2)
	got, _ := d.ClaimReminders(ctx, "a", now, now.Add(claimTTL), 10)
	if len(got) != 1 {
		t.Fatalf("claim = %v", ids(got))
	}
	// Wrong token and empty token lose.
	for _, tok := range []string{"other", ""} {
		bad := got[0].Reminder
		bad.ClaimToken = tok
		if ok, err := d.RescheduleClaimed(ctx, bad); err != nil || ok {
			t.Fatalf("token %q: ok=%v err=%v", tok, ok, err)
		}
	}
	mine := got[0].Reminder
	mine.FireAt, mine.Attempts = now.Add(time.Minute), 1
	if ok, err := d.RescheduleClaimed(ctx, mine); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	g := getReminder(t, d, r.ID)
	if !g.FireAt.Equal(now.Add(time.Minute)) || g.Attempts != 1 || g.State != model.ReminderPending {
		t.Fatalf("after reschedule: %+v", g)
	}
	// The claim is gone: not a second time, but the row is claimable again.
	if ok, _ := d.RescheduleClaimed(ctx, mine); ok {
		t.Fatal("reschedule succeeded twice")
	}
	if again, _ := d.ClaimReminders(ctx, "b", now.Add(time.Minute), now.Add(claimTTL), 10); len(again) != 1 {
		t.Fatalf("rescheduled reminder not claimable: %v", ids(again))
	}
	// Permanent failure through RescheduleClaimed.
	failed := again0(t, d, "c", now.Add(2*claimTTL))
	failed.State = model.ReminderFailed
	if ok, err := d.RescheduleClaimed(ctx, failed); err != nil || !ok {
		t.Fatalf("fail: ok=%v err=%v", ok, err)
	}
	if g := getReminder(t, d, r.ID); g.State != model.ReminderFailed {
		t.Fatalf("state = %s", g.State)
	}
	if _, ok, _ := d.NextReminderAt(ctx); ok {
		t.Fatal("failed reminder still counted")
	}
}

// again0 claims the single due reminder with token.
func again0(t *testing.T, d store.Store, token string, now time.Time) model.Reminder {
	t.Helper()
	got, err := d.ClaimReminders(context.Background(), token, now, now.Add(claimTTL), 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("claim = %v, %v", ids(got), err)
	}
	return got[0].Reminder
}

// A claimer whose claim expired must not undo what the new owner did.
func testStaleClaimerReminder(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	r := reminder(t, a, 1, model.ReminderDue)
	now := *tp(2)
	ra := again0(t, a, "A", now)
	later := now.Add(claimTTL) // A's claim has expired
	rb := again0(t, b, "B", later)
	if ok, err := b.MarkReminderSent(ctx, rb.ID, 77, later); err != nil || !ok {
		t.Fatalf("B mark sent: ok=%v err=%v", ok, err)
	}
	ra.FireAt, ra.Attempts = later.Add(time.Minute), 1
	if ok, err := a.RescheduleClaimed(ctx, ra); err != nil || ok {
		t.Fatalf("stale reschedule: ok=%v err=%v", ok, err)
	}
	g := getReminder(t, a, r.ID)
	if g.State != model.ReminderSent || g.MessageID != 77 || g.Attempts != 0 {
		t.Fatalf("stale claimer changed the row: %+v", g)
	}
	if ok, _ := a.MarkReminderSent(ctx, r.ID, 1, later); ok {
		t.Fatal("second MarkReminderSent succeeded")
	}

	// Same, but the new owner has only re-claimed (not sent): A still loses.
	r2 := reminder(t, a, 1, model.ReminderCustom)
	ra2 := again0(t, a, "A2", later.Add(time.Hour))
	rb2 := again0(t, b, "B2", later.Add(time.Hour+claimTTL))
	if ok, _ := a.RescheduleClaimed(ctx, ra2); ok {
		t.Fatal("expired claim rescheduled over a new claim")
	}
	if ok, _ := b.RescheduleClaimed(ctx, rb2); !ok {
		t.Fatal("new owner lost its own claim")
	}
	_ = r2
}

func testMarkReminderSentConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	r := reminder(t, hs[0], 1, model.ReminderDue)
	var mu sync.Mutex
	wins := 0
	at := *tp(2)
	parallel(8, func(i int) {
		ok, err := hs[i%len(hs)].MarkReminderSent(ctx, r.ID, 100+i, at)
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
		t.Fatalf("%d winners, want 1", wins)
	}
	g := getReminder(t, hs[0], r.ID)
	if g.State != model.ReminderSent || g.SentAt == nil || !g.SentAt.Equal(at) || g.MessageID < 100 {
		t.Fatalf("row = %+v", g)
	}
	// Cancelled reminders cannot be marked sent.
	c := reminder(t, hs[0], 1, model.ReminderCustom)
	if err := hs[0].CancelPending(ctx, c.TodoID); err != nil {
		t.Fatal(err)
	}
	if ok, err := hs[1].MarkReminderSent(ctx, c.ID, 1, at); err != nil || ok {
		t.Fatalf("cancelled: ok=%v err=%v", ok, err)
	}
	if ok, err := hs[1].MarkReminderSent(ctx, 99999, 1, at); err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}

func testNextReminderAt(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d, o := open(), open() // d claims, o (another node) reads
	if _, ok, err := d.NextReminderAt(ctx); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	reminder(t, d, 1, model.ReminderDue)
	reminder(t, d, 5, model.ReminderCustom)
	next, ok, err := d.NextReminderAt(ctx)
	if err != nil || !ok || !next.Equal(*tp(1)) || next.Location() != time.UTC {
		t.Fatalf("unclaimed: %v %v %v", next, ok, err)
	}
	// Claim the first one for 10 minutes: it counts from claim_until, but
	// not before the other reminder's fire_at.
	now := *tp(2)
	until := now.Add(10 * time.Minute)
	if got, _ := d.ClaimReminders(ctx, "a", now, until, 1); len(got) != 1 {
		t.Fatalf("claim = %v", ids(got))
	}
	next, ok, _ = o.NextReminderAt(ctx)
	if !ok || !next.Equal(until) {
		t.Fatalf("claimed: next = %v, want %v", next, until)
	}
	// Re-claim it (the first claim expired) far into the future: max()
	// makes the other reminder, due at tp(5), the next one.
	if got, _ := d.ClaimReminders(ctx, "b", now.Add(11*time.Minute), *tp(8), 1); len(got) != 1 {
		t.Fatalf("claim b = %v", ids(got))
	}
	next, _, _ = o.NextReminderAt(ctx)
	if !next.Equal(*tp(5)) {
		t.Fatalf("next = %v, want %v", next, *tp(5))
	}
}

// ---- digest claims ----

func digestChat(t *testing.T, d store.Store, id int64, next time.Time) {
	t.Helper()
	chat(t, d, id)
	if err := d.SetDigest(context.Background(), id, "08:00", 7, &next); err != nil {
		t.Fatal(err)
	}
}

func chatIDs(cs []model.Chat) []int64 {
	out := make([]int64, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func testClaimDigests(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	digestChat(t, d, 1, *tp(1))
	digestChat(t, d, 2, *tp(2))
	digestChat(t, d, 3, *tp(9))
	chat(t, d, 4) // no digest

	now := *tp(3)
	got, err := d.ClaimDigests(ctx, "a", now, now.Add(claimTTL), 1)
	if err != nil || len(got) != 1 || got[0].ID != 1 || got[0].DigestClaimToken != "a" {
		t.Fatalf("claim = %+v, %v", got, err)
	}
	got, _ = d.ClaimDigests(ctx, "b", now, now.Add(claimTTL), 10)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("second claim = %v", chatIDs(got))
	}
	if got, _ = d.ClaimDigests(ctx, "c", now.Add(claimTTL-time.Second), now.Add(2*claimTTL), 10); len(got) != 0 {
		t.Fatalf("claims visible before expiry: %v", chatIDs(got))
	}
	got, _ = d.ClaimDigests(ctx, "d", now.Add(claimTTL), now.Add(2*claimTTL), 10)
	if len(got) != 2 || got[0].DigestClaimToken != "d" {
		t.Fatalf("expired claims not retaken: %v", chatIDs(got))
	}

	// AdvanceDigest: wrong token loses, right token wins and frees the chat.
	if ok, err := d.AdvanceDigest(ctx, 1, "a", *tp(30)); err != nil || ok {
		t.Fatalf("old token: ok=%v err=%v", ok, err)
	}
	if ok, err := d.AdvanceDigest(ctx, 1, "d", *tp(30)); err != nil || !ok {
		t.Fatalf("right token: ok=%v err=%v", ok, err)
	}
	if ok, _ := d.AdvanceDigest(ctx, 1, "d", *tp(31)); ok {
		t.Fatal("advanced twice")
	}
	c, _ := d.GetChat(ctx, 1)
	if c.DigestNextAt == nil || !c.DigestNextAt.Equal(*tp(30)) || c.DigestTime != "08:00" {
		t.Fatalf("chat = %+v", c)
	}
	if ok, _ := d.AdvanceDigest(ctx, 4, "d", *tp(30)); ok {
		t.Fatal("unclaimed chat advanced")
	}

	// SetDigest clears a claim, so the old claimer loses.
	if err := d.SetDigest(ctx, 2, "09:00", 7, tp(3)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.AdvanceDigest(ctx, 2, "d", *tp(40)); ok {
		t.Fatal("claim survived SetDigest")
	}
	if got, _ := d.ClaimDigests(ctx, "e", now, now.Add(claimTTL), 10); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("after SetDigest: %v", chatIDs(got))
	}

	if _, err := d.ClaimDigests(ctx, "", now, now.Add(claimTTL), 5); err == nil {
		t.Fatal("empty token must be an error")
	}
	if _, err := d.ClaimDigests(ctx, "", now, now.Add(claimTTL), 0); err == nil {
		t.Fatal("empty token must be an error, also with limit 0")
	}

	// A second node's NextDigestAt counts a claimed chat from its claim_until.
	next, ok, err := open().NextDigestAt(ctx)
	want := now.Add(claimTTL) // chat 2 claimed by "e"; chat 3 is at tp(9)
	if err != nil || !ok || !next.Equal(want) {
		t.Fatalf("next = %v %v %v, want %v", next, ok, err, want)
	}
}

func testClaimDigestsConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 3)
	const n = 100
	for i := range n {
		digestChat(t, hs[0], int64(i+1), *tp(1))
	}
	now := *tp(2)
	claimAll(t, hs, n, func(h store.Store, token string, limit int) ([]int64, error) {
		got, err := h.ClaimDigests(ctx, token, now, now.Add(claimTTL), limit)
		for _, c := range got {
			if c.DigestClaimToken != token {
				t.Errorf("token = %q, want %q", c.DigestClaimToken, token)
			}
		}
		return chatIDs(got), err
	})
}

func testStaleClaimerDigest(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	digestChat(t, a, 1, *tp(1))
	now := *tp(2)
	if got, _ := a.ClaimDigests(ctx, "A", now, now.Add(claimTTL), 10); len(got) != 1 {
		t.Fatalf("A claim = %v", chatIDs(got))
	}
	later := now.Add(claimTTL)
	if got, _ := b.ClaimDigests(ctx, "B", later, later.Add(claimTTL), 10); len(got) != 1 {
		t.Fatalf("B claim = %v", chatIDs(got))
	}
	if ok, err := b.AdvanceDigest(ctx, 1, "B", *tp(26)); err != nil || !ok {
		t.Fatalf("B advance: ok=%v err=%v", ok, err)
	}
	if ok, err := a.AdvanceDigest(ctx, 1, "A", *tp(50)); err != nil || ok {
		t.Fatalf("stale advance: ok=%v err=%v", ok, err)
	}
	c, _ := a.GetChat(ctx, 1)
	if c.DigestNextAt == nil || !c.DigestNextAt.Equal(*tp(26)) {
		t.Fatalf("stale claimer changed digest_next_at: %+v", c)
	}
}

// ---- lease ----

func testLease(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	const ttl = 30 * time.Second
	now := base

	l, err := a.AcquireLease(ctx, "poller", "A", now, ttl)
	if err != nil || l.Holder != "A" || l.Epoch != 1 || l.Name != "poller" || !l.Until.Equal(now.Add(ttl)) {
		t.Fatalf("first acquire = %+v, %v", l, err)
	}
	// Live: another holder gets the row back, unchanged, and no error.
	l, err = b.AcquireLease(ctx, "poller", "B", now.Add(ttl-time.Second), ttl)
	if err != nil || l.Holder != "A" || l.Epoch != 1 || !l.Until.Equal(now.Add(ttl)) {
		t.Fatalf("B while live = %+v, %v", l, err)
	}
	// Renewal by the holder keeps the epoch and extends the lease.
	l, _ = a.AcquireLease(ctx, "poller", "A", now.Add(10*time.Second), ttl)
	if l.Holder != "A" || l.Epoch != 1 || !l.Until.Equal(now.Add(10*time.Second+ttl)) {
		t.Fatalf("renewal = %+v", l)
	}
	// Expiry is "<= now": at exactly Until the lease is free.
	exp := now.Add(10*time.Second + ttl)
	l, _ = b.AcquireLease(ctx, "poller", "B", exp, ttl)
	if l.Holder != "B" || l.Epoch != 2 {
		t.Fatalf("B at expiry = %+v", l)
	}
	// A holder re-taking its own expired lease gets a new epoch.
	l, _ = b.AcquireLease(ctx, "poller", "B", exp.Add(ttl), ttl)
	if l.Holder != "B" || l.Epoch != 3 {
		t.Fatalf("B own expired = %+v", l)
	}
	// ReleaseLease by a non-holder is a no-op.
	at := exp.Add(ttl + time.Second)
	if err := a.ReleaseLease(ctx, "poller", "A"); err != nil {
		t.Fatal(err)
	}
	if l, _ = a.AcquireLease(ctx, "poller", "A", at, ttl); l.Holder != "B" {
		t.Fatalf("release by non-holder freed the lease: %+v", l)
	}
	// Release by the holder frees it at once; the next holder bumps the epoch.
	if err := b.ReleaseLease(ctx, "poller", "B"); err != nil {
		t.Fatal(err)
	}
	l, _ = a.AcquireLease(ctx, "poller", "A", at, ttl)
	if l.Holder != "A" || l.Epoch != 4 {
		t.Fatalf("A after release = %+v", l)
	}
	// Releasing a missing lease is fine, and leases are independent by name.
	if err := a.ReleaseLease(ctx, "nothing", "A"); err != nil {
		t.Fatal(err)
	}
	if l, _ = b.AcquireLease(ctx, "setup", "B", at, ttl); l.Holder != "B" || l.Epoch != 1 {
		t.Fatalf("other lease = %+v", l)
	}
}

// leaseRace has 8 contenders (2 per handle) acquire at the same instant and
// returns the single winner's row. All callers must see the same row.
func leaseRace(t *testing.T, hs []store.Store, now time.Time) model.Lease {
	t.Helper()
	const n = 8
	res := make([]model.Lease, n)
	parallel(n, func(i int) {
		l, err := hs[i%len(hs)].AcquireLease(context.Background(), "poller", fmt.Sprintf("n%d", i), now, time.Minute)
		if err != nil {
			t.Error(err)
		}
		res[i] = l
	})
	winners := 0
	for i, l := range res {
		if l.Holder == fmt.Sprintf("n%d", i) {
			winners++
		}
		if l != res[0] {
			t.Fatalf("callers disagree: %+v", res)
		}
	}
	if winners != 1 {
		t.Fatalf("%d winners, want 1: %+v", winners, res)
	}
	return res[0]
}

func testLeaseConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	l := leaseRace(t, hs, base)
	if l.Epoch != 1 || !l.Until.Equal(base.Add(time.Minute)) {
		t.Fatalf("first race = %+v", l)
	}
	// The holder renews: same epoch.
	r, err := hs[0].AcquireLease(ctx, "poller", l.Holder, base.Add(10*time.Second), time.Minute)
	if err != nil || r.Holder != l.Holder || r.Epoch != 1 {
		t.Fatalf("renewal = %+v, %v", r, err)
	}
	// At expiry (<= now) exactly one new holder, epoch+1.
	exp := r.Until
	l2 := leaseRace(t, hs, exp)
	if l2.Epoch != 2 {
		t.Fatalf("after expiry = %+v", l2)
	}
	// After ReleaseLease the lease is free at the same instant: one winner, epoch+1.
	if err := hs[1].ReleaseLease(ctx, "poller", l2.Holder); err != nil {
		t.Fatal(err)
	}
	if l3 := leaseRace(t, hs, exp); l3.Epoch != 3 {
		t.Fatalf("after release = %+v", l3)
	}
}

// ---- prompts ----

func prompt(msg int, user int64, expH int) model.Prompt {
	return model.Prompt{ChatID: 5, MsgID: msg, UserID: user, Kind: model.PromptRemind, TodoID: 9, ExpiresAt: *tp(expH)}
}

func testPrompts(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	now := *tp(1)
	if _, err := d.TakePrompt(ctx, 5, 1, 7, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := d.PutPrompt(ctx, prompt(1, 7, 2)); err != nil {
		t.Fatal(err)
	}
	// Wrong user: ErrNotFound, and the right user can still take it.
	if _, err := d.TakePrompt(ctx, 5, 1, 8, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong user: %v", err)
	}
	got, err := d.TakePrompt(ctx, 5, 1, 7, now)
	want := prompt(1, 7, 2)
	if err != nil || got != want {
		t.Fatalf("take = %+v, %v; want %+v", got, err, want)
	}
	if _, err := d.TakePrompt(ctx, 5, 1, 7, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second take: %v", err)
	}

	// Put upserts on (chat, msg).
	if err := d.PutPrompt(ctx, prompt(2, 7, 2)); err != nil {
		t.Fatal(err)
	}
	p := prompt(2, 8, 3)
	p.Kind, p.TodoID = model.PromptEdit, 10
	if err := d.PutPrompt(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, err = d.TakePrompt(ctx, 5, 2, 8, now); err != nil || got != p {
		t.Fatalf("upserted take = %+v, %v; want %+v", got, err, p)
	}
	// Same message id in another chat is a different prompt.
	other := prompt(2, 7, 2)
	other.ChatID = 6
	if err := d.PutPrompt(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, err = d.TakePrompt(ctx, 6, 2, 7, now); err != nil || got != other {
		t.Fatalf("other chat = %+v, %v", got, err)
	}

	// Expired (<= now): ErrNotFound, the row stays until Purge.
	if err := d.PutPrompt(ctx, prompt(3, 7, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TakePrompt(ctx, 5, 3, 7, *tp(2)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired at boundary: %v", err)
	}
	if got, err = d.TakePrompt(ctx, 5, 3, 7, now); err != nil || got.MsgID != 3 {
		t.Fatalf("expired row was deleted by Take: %+v, %v", got, err)
	}
}

func testTakePromptConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	if err := hs[0].PutPrompt(ctx, prompt(1, 7, 2)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	wins := 0
	parallel(8, func(i int) {
		_, err := hs[i%len(hs)].TakePrompt(ctx, 5, 1, 7, *tp(1))
		switch {
		case err == nil:
			mu.Lock()
			wins++
			mu.Unlock()
		case !errors.Is(err, store.ErrNotFound):
			t.Error(err)
		}
	})
	if wins != 1 {
		t.Fatalf("%d winners, want 1", wins)
	}
}

// ---- update dedupe and purge ----

func testMarkUpdate(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	for i, want := range []bool{true, false, false} {
		first, err := d.MarkUpdate(ctx, 1001, base.Add(time.Duration(i)*time.Second))
		if err != nil || first != want {
			t.Fatalf("call %d: first=%v err=%v, want %v", i, first, err, want)
		}
	}
	if first, _ := d.MarkUpdate(ctx, 1002, base); !first {
		t.Fatal("different id must be first")
	}
}

func testMarkUpdateConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	var mu sync.Mutex
	firsts := map[int64]int{}
	parallel(16, func(i int) {
		id := int64(i % 4)
		first, err := hs[i%len(hs)].MarkUpdate(ctx, id, base)
		if err != nil {
			t.Error(err)
		}
		if first {
			mu.Lock()
			firsts[id]++
			mu.Unlock()
		}
	})
	for id := range int64(4) {
		if firsts[id] != 1 {
			t.Fatalf("update %d first %d times: %v", id, firsts[id], firsts)
		}
	}
}

func testPurge(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	a, b := open(), open()
	// Prompts: expired ones go, live ones stay (expiry is <= now).
	for msg, h := range map[int]int{1: 1, 2: 2, 3: 3} {
		if err := a.PutPrompt(ctx, prompt(msg, 7, h)); err != nil {
			t.Fatal(err)
		}
	}
	// Update ids: kept for 48h; at exactly 48h they are purged.
	for id, age := range map[int64]time.Duration{1: 72 * time.Hour, 2: 48 * time.Hour, 3: 47 * time.Hour} {
		if first, err := a.MarkUpdate(ctx, id, base.Add(-age)); err != nil || !first {
			t.Fatalf("seed update %d: %v %v", id, first, err)
		}
	}
	// Purge at base: cutoff is base-48h.
	if err := b.Purge(ctx, base); err != nil {
		t.Fatal(err)
	}
	for id, wantFirst := range map[int64]bool{1: true, 2: true, 3: false} {
		first, err := a.MarkUpdate(ctx, id, base)
		if err != nil || first != wantFirst {
			t.Fatalf("update %d after Purge: first=%v err=%v, want %v", id, first, err, wantFirst)
		}
	}
	// Purge at tp(2): prompts 1 and 2 are expired (<= now), 3 is live.
	if err := b.Purge(ctx, *tp(2)); err != nil {
		t.Fatal(err)
	}
	early := base
	if _, err := a.TakePrompt(ctx, 5, 1, 7, early); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired prompt 1 survived Purge: %v", err)
	}
	if _, err := a.TakePrompt(ctx, 5, 2, 7, early); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("prompt expiring exactly now survived Purge: %v", err)
	}
	if _, err := a.TakePrompt(ctx, 5, 3, 7, early); err != nil {
		t.Fatalf("live prompt purged: %v", err)
	}
}
