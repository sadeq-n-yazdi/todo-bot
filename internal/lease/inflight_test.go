package lease_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

func newHang(t *testing.T, path string, passthrough bool) *hangStore {
	t.Helper()
	h := &hangStore{Store: openDB(t, path), entered: make(chan struct{}, 1),
		unblock: make(chan struct{}), passthrough: passthrough}
	return h
}

func (h *hangStore) release() { h.once.Do(func() { close(h.unblock) }) }

func TestOneAcquireInFlight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	h := newHang(t, path, false)
	t.Cleanup(h.release)
	a := startStore(t, clk, &tracker{}, h, "a", 0)
	eventually(t, "a leads", a.leading)
	h.hang.Store(true)
	clk.Advance(ttl / 3) // t=10: the renewal hangs and ignores ctx
	<-h.entered
	clk.Advance(ttl / 3) // t=20: fenced
	eventually(t, "fence", func() bool { return !a.leading() })
	for i := 0; i < 12; i++ {
		advance(t, clk, ttl/3, a)
	}
	if got := h.hung.Load(); got != 1 {
		t.Fatalf("%d hanging calls started, want 1", got)
	}
	if got := h.maxIn.Load(); got != 1 {
		t.Fatalf("max calls in flight = %d, want 1", got)
	}
}

func TestStaleResultNeverRevivesLeadership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	h := newHang(t, path, true)
	t.Cleanup(h.release)
	a := startStore(t, clk, &tracker{}, h, "a", 0)
	eventually(t, "a leads", a.leading)
	h.hang.Store(true)
	clk.Advance(ttl / 3)
	<-h.entered
	clk.Advance(ttl / 3) // t=20: fenced while the call hangs
	eventually(t, "fence", func() bool { return !a.leading() })
	h.hang.Store(false)
	h.down.Store(true) // fresh calls fail, so only the stale result could revive a
	h.release()        // the old call now succeeds, as a late write
	eventually(t, "old call to return", func() bool { return h.inflight.Load() == 0 })
	advance(t, clk, ttl/3, a) // t=30: the stale result is drained, the fresh call fails
	if a.leading() || len(a.starts()) != 1 {
		t.Fatalf("stale result revived leadership: leading=%v starts=%v", a.leading(), a.starts())
	}
}

// The sequence from the review: a's renewal hangs at t=10 and its late write
// lands afterwards while b joins. The two must never lead together.
func TestHungRenewalNeverCausesTwoLeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	h := newHang(t, path, true)
	t.Cleanup(h.release)
	a := startStore(t, clk, tr, h, "a", 0)
	eventually(t, "a leads", a.leading)
	b := start(t, clk, tr, path, "b", 0)
	settle(t, clk, b)
	h.hang.Store(true)
	clk.Advance(ttl / 3) // t=10: a's call hangs
	<-h.entered
	settle(t, clk, b)
	clk.Advance(ttl / 3) // t=20: a fences
	eventually(t, "fence", func() bool { return !a.leading() })
	h.hang.Store(false)
	advance(t, clk, ttl/3, b) // t=30: a's lease expired; b takes over
	h.release()               // a's stale write lands
	for _, d := range []time.Duration{ttl / 3, 2 * time.Second} {
		advance(t, clk, d, b) // t=40, t=42
	}
	if m := tr.max.Load(); m > 1 {
		t.Fatalf("max concurrent leaders = %d, want 1", m)
	}
}

// heldUntil reads the lease's held_until. A probe far in the past never wins,
// so it only reads the row.
func heldUntil(t *testing.T, st store.Store) time.Time {
	t.Helper()
	l, err := st.AcquireLease(context.Background(), leaseKey, "probe:1", t0.Add(-time.Hour), ttl)
	if err != nil {
		t.Fatal(err)
	}
	return l.Until
}

// The shrink case: a's cancelled renewal (now=10) is still queued in the
// database. It lands after a recovered and renewed, and would move held_until
// back from 60 to 40. Then a fails once at t=40 and b joins. If held_until
// shrank, b would win at once while a still leads.
func TestHungRenewalNeverCausesTwoLeadersAfterShrink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	h := newHang(t, path, true)
	h.honourCtx, h.lateWrite = true, true
	t.Cleanup(func() { h.release(); h.late.Wait() })
	a := startStore(t, clk, tr, h, "a", 0)
	eventually(t, "a leads", a.leading)
	probe := openDB(t, path)
	last := heldUntil(t, probe)
	check := func(step string) {
		t.Helper()
		got := heldUntil(t, probe)
		if got.Before(last) {
			t.Fatalf("%s: held_until went back from %v to %v", step, last, got)
		}
		last = got
		if m := tr.max.Load(); m > 1 {
			t.Fatalf("%s: %d leaders at once", step, m)
		}
	}

	h.hang.Store(true)
	clk.Advance(ttl / 3) // t=10: the renewal hangs
	<-h.entered
	clk.Advance(5 * time.Second) // t=15: a fences and cancels the call
	eventually(t, "fence", func() bool { return !a.leading() })
	eventually(t, "call to give up", func() bool { return h.inflight.Load() == 0 })
	h.hang.Store(false)
	advance(t, clk, 5*time.Second, a) // t=20: a recovers
	eventually(t, "a leads again", a.leading)
	check("recovery")
	advance(t, clk, ttl/3, a) // t=30: a renews, held_until = 60
	check("renewal")
	h.release() // the queued write lands: now=10, held_until would be 40
	h.late.Wait()
	check("late write")
	h.down.Store(true)
	advance(t, clk, ttl/3, a) // t=40: a's renewal fails once
	h.down.Store(false)
	b := start(t, clk, tr, path, "b", 0) // b joins at t=40
	settle(t, clk, a, b)
	check("b joined")
	if !a.leading() || b.leading() {
		t.Fatalf("a leading=%v, b leading=%v; want a only", a.leading(), b.leading())
	}
	advance(t, clk, ttl/3, a, b) // t=50
	check("t=50")
	if m := tr.max.Load(); m != 1 {
		t.Fatalf("max concurrent leaders = %d, want 1", m)
	}
}
