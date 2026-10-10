package lease_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
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
