package lease_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/lease"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite"
)

const (
	ttl      = 30 * time.Second
	leaseKey = "poller"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// tracker is shared by the nodes of one test. It records the leaders running
// at the same time across all nodes.
type tracker struct {
	cur atomic.Int64
	max atomic.Int64
}

func (tr *tracker) enter() {
	c := tr.cur.Add(1)
	for {
		m := tr.max.Load()
		if c <= m || tr.max.CompareAndSwap(m, c) {
			return
		}
	}
}

func (tr *tracker) exit() { tr.cur.Add(-1) }

// flakyStore misbehaves on demand.
type flakyStore struct {
	store.Store
	fail atomic.Bool
	lost atomic.Bool // report another holder, as if it had taken over
}

func (f *flakyStore) AcquireLease(ctx context.Context, name, holder string, now time.Time, d time.Duration) (model.Lease, error) {
	if f.fail.Load() {
		return model.Lease{}, errors.New("store down")
	}
	if f.lost.Load() {
		return model.Lease{Name: name, Holder: "other:1", Until: now.Add(d), Epoch: 99}, nil
	}
	return f.Store.AcquireLease(ctx, name, holder, now, d)
}

func (f *flakyStore) ReleaseLease(ctx context.Context, name, holder string) error {
	if f.fail.Load() {
		return errors.New("store down")
	}
	return f.Store.ReleaseLease(ctx, name, holder)
}

type node struct {
	st     *flakyStore  // nil unless the node uses a flakyStore
	next   atomic.Int64 // last idle report, UnixNano
	exited atomic.Bool  // Run has returned
	cancel context.CancelFunc
	done   chan error

	mu      sync.Mutex
	epochs  []int64
	running int
}

func openDB(t *testing.T, path string) store.Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// start runs a loop on a flakyStore whose first earlyReturns lead calls return at once.
func start(t *testing.T, clk *clocktest.Fake, tr *tracker, path, name string, earlyReturns int) *node {
	t.Helper()
	fs := &flakyStore{Store: openDB(t, path)}
	n := startStore(t, clk, tr, fs, name, earlyReturns)
	n.st = fs
	return n
}

func startStore(t *testing.T, clk *clocktest.Fake, tr *tracker, st store.Store, name string, earlyReturns int) *node {
	t.Helper()
	n := &node{done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	t.Cleanup(cancel)
	go func() {
		err := lease.RunWithIdle(ctx, st, clk, leaseKey, name, ttl, func(lctx context.Context, epoch int64) {
			tr.enter()
			n.mu.Lock()
			n.epochs = append(n.epochs, epoch)
			calls := len(n.epochs)
			n.running++
			n.mu.Unlock()
			if calls > earlyReturns {
				<-lctx.Done()
			}
			n.mu.Lock()
			n.running--
			n.mu.Unlock()
			tr.exit()
		}, func(next time.Time) { n.next.Store(next.UnixNano()) })
		n.exited.Store(true)
		n.done <- err
	}()
	return n
}

func (n *node) leading() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running > 0
}

func (n *node) starts() []int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]int64(nil), n.epochs...)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// settle waits until every node is idle with nothing due at or before now:
// each has reported a next wake-up time after the current fake time.
func settle(t *testing.T, clk *clocktest.Fake, nodes ...*node) {
	t.Helper()
	eventually(t, "loops to go idle", func() bool {
		now := clk.Now().UnixNano()
		for _, n := range nodes {
			if n.exited.Load() {
				t.Fatal("settle: a node's Run has returned; it will never go idle")
			}
			if n.next.Load() <= now {
				return false
			}
		}
		return true
	})
}

// advance moves the clock by d and waits until every node has handled
// everything that fell due.
func advance(t *testing.T, clk *clocktest.Fake, d time.Duration, nodes ...*node) {
	t.Helper()
	clk.Advance(d)
	settle(t, clk, nodes...)
}

func countLeaders(ns ...*node) int {
	c := 0
	for _, n := range ns {
		if n.leading() {
			c++
		}
	}
	return c
}

func TestSingleLeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 0)
	b := start(t, clk, tr, path, "b", 0)
	c := start(t, clk, tr, path, "a", 0) // same NodeID as a: must still not co-lead
	nodes := []*node{a, b, c}
	eventually(t, "a leader", func() bool { return countLeaders(nodes...) == 1 })
	settle(t, clk, nodes...)
	for i := 0; i < 10; i++ {
		advance(t, clk, ttl/3, nodes...)
		if got := countLeaders(nodes...); got != 1 {
			t.Fatalf("round %d: %d leaders, want 1", i, got)
		}
	}
	total := 0
	for _, n := range nodes {
		total += len(n.starts())
	}
	if total != 1 {
		t.Fatalf("lead started %d times, want 1 (renewals must not restart it)", total)
	}
	if m := tr.max.Load(); m != 1 {
		t.Fatalf("max concurrent leaders = %d, want 1", m)
	}
}

func TestTakeoverAfterTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 0)
	eventually(t, "a leads", a.leading)
	a.st.fail.Store(true) // a can neither renew nor release
	b := start(t, clk, tr, path, "b", 0)
	settle(t, clk, a, b)
	if b.leading() {
		t.Fatal("b leads while a holds the lease")
	}
	for i := 0; i < 4 && !b.leading(); i++ {
		advance(t, clk, ttl/3, a, b)
	}
	eventually(t, "b takes over", b.leading)
	eventually(t, "a stops", func() bool { return !a.leading() })
	if got := b.starts(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("b epochs = %v, want [2]", got)
	}
	if m := tr.max.Load(); m != 1 {
		t.Fatalf("max concurrent leaders = %d, want 1", m)
	}
}

func TestFenceAfterHalfTTLWithoutRenewal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 0)
	eventually(t, "a leads", a.leading)
	a.st.fail.Store(true)

	advance(t, clk, ttl/3, a) // t=10: 10s since the last renewal, within ttl/2
	if !a.leading() {
		t.Fatal("fenced too early")
	}
	advance(t, clk, ttl/3, a) // t=20: the fence point (15s) has passed
	eventually(t, "fence", func() bool { return !a.leading() })

	// The store recovers at t=30. The lease expired at t=30 (held_until <= now),
	// so a re-takes it with a new epoch and leads again.
	a.st.fail.Store(false)
	advance(t, clk, ttl/3, a)
	eventually(t, "re-lead", a.leading)
	if got := a.starts(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("epochs = %v, want [1 2]", got)
	}
}

func TestLeadContextCancelledWhenLost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 0)
	eventually(t, "a leads", a.leading)
	a.st.lost.Store(true)
	advance(t, clk, ttl/3, a) // well inside ttl/2: only the loss can stop it
	eventually(t, "a loses lead", func() bool { return !a.leading() })
}

func TestLeadReturningEarlyIsRestarted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 1) // the first lead call returns at once
	eventually(t, "first lead", func() bool { return len(a.starts()) == 1 })
	settle(t, clk, a)
	advance(t, clk, ttl/3, a)
	eventually(t, "restart", func() bool { return len(a.starts()) == 2 && a.leading() })
	if got := a.starts(); got[0] != got[1] {
		t.Fatalf("epochs = %v, want the same epoch twice", got)
	}
	if m := tr.max.Load(); m != 1 {
		t.Fatalf("max concurrent lead calls = %d, want 1", m)
	}
}

func TestShutdownReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	a := start(t, clk, &tracker{}, path, "a", 0)
	eventually(t, "a leads", a.leading)
	a.cancel()
	select {
	case err := <-a.done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if a.leading() {
		t.Fatal("lead still running after Run returned")
	}
	other := openDB(t, path)
	l, err := other.AcquireLease(context.Background(), leaseKey, "b:1", clk.Now(), ttl)
	if err != nil {
		t.Fatal(err)
	}
	if l.Holder != "b:1" {
		t.Fatalf("lease not released: holder %q", l.Holder)
	}
}

func TestRenewedEveryThirdOfTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 0)
	eventually(t, "a leads", a.leading)
	other := openDB(t, path)
	for i := 0; i < 3; i++ {
		advance(t, clk, ttl/3, a)
		want := clk.Now().Add(ttl)
		eventually(t, "renewal", func() bool {
			// A probe far in the past never wins, so it only reads the row.
			l, err := other.AcquireLease(context.Background(), leaseKey, "probe:1", t0.Add(-time.Hour), ttl)
			return err == nil && l.Until.Equal(want)
		})
	}
}

// hangStore blocks AcquireLease while hang is set, like a stuck read. If
// honourCtx is set it returns when its context is cancelled; otherwise it
// ignores the context.
type hangStore struct {
	store.Store
	hang       atomic.Bool
	down       atomic.Bool // non-hanging calls fail
	honourCtx  bool
	entered    chan struct{}
	unblock    chan struct{}
	ctxWasDone atomic.Bool

	passthrough bool // after unblock, perform the real call (a late write)
	once        sync.Once
	hung        atomic.Int64 // hanging calls started
	inflight    atomic.Int64 // calls running now, hanging or not
	maxIn       atomic.Int64 // most calls that ran at once
	lateWrite   bool         // with honourCtx: a cancelled call still writes when unblocked
	late        sync.WaitGroup
}

func (h *hangStore) AcquireLease(ctx context.Context, name, holder string, now time.Time, d time.Duration) (model.Lease, error) {
	cur := h.inflight.Add(1) // every call counts, so maxIn is the real concurrency
	defer h.inflight.Add(-1)
	for m := h.maxIn.Load(); cur > m && !h.maxIn.CompareAndSwap(m, cur); m = h.maxIn.Load() {
	}
	if h.hang.Load() {
		h.hung.Add(1)
		select {
		case h.entered <- struct{}{}:
		default:
		}
		if h.honourCtx {
			select {
			case <-ctx.Done():
				h.ctxWasDone.Store(true)
				if h.lateWrite {
					// The caller gave up, but the write is still queued
					// in the database and lands when unblocked.
					h.late.Add(1)
					go func() {
						defer h.late.Done()
						<-h.unblock
						_, _ = h.Store.AcquireLease(context.WithoutCancel(ctx), name, holder, now, d)
					}()
				}
				return model.Lease{}, ctx.Err()
			case <-h.unblock:
			}
		} else {
			<-h.unblock
		}
		if h.passthrough {
			return h.Store.AcquireLease(context.WithoutCancel(ctx), name, holder, now, d)
		}
		return model.Lease{}, errors.New("timeout")
	}
	if h.down.Load() {
		return model.Lease{}, errors.New("store down")
	}
	return h.Store.AcquireLease(ctx, name, holder, now, d)
}

func TestFenceWhileAcquireHangs(t *testing.T) {
	for _, honour := range []bool{false, true} {
		name := "ignores ctx"
		if honour {
			name = "honours ctx"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db.sqlite")
			clk := clocktest.New(t0)
			h := &hangStore{Store: openDB(t, path), honourCtx: honour,
				entered: make(chan struct{}, 1), unblock: make(chan struct{})}
			t.Cleanup(func() { close(h.unblock) })
			var aLeading atomic.Bool
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			go func() {
				_ = lease.Run(ctx, h, clk, leaseKey, "a", ttl, func(lctx context.Context, _ int64) {
					aLeading.Store(true)
					<-lctx.Done()
					aLeading.Store(false)
				})
			}()
			eventually(t, "a leads", aLeading.Load)
			h.hang.Store(true)
			clk.Advance(ttl / 3) // t=10: the renewal hangs
			select {
			case <-h.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("renewal did not start")
			}
			if !aLeading.Load() {
				t.Fatal("fenced too early")
			}
			clk.Advance(ttl / 2) // t=25: past lastOK+ttl/2, before ttl
			eventually(t, "fence while the call hangs", func() bool { return !aLeading.Load() })
			if honour {
				eventually(t, "call cancelled", h.ctxWasDone.Load)
			}
		})
	}
}
