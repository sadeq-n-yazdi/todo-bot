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

// tracker is shared by the nodes of one test. It counts AcquireLease calls
// (to know when every loop has run its attempt) and the leaders running at
// the same time across all nodes.
type tracker struct {
	calls atomic.Int64
	done  atomic.Int64 // AcquireLease calls that have returned
	cur   atomic.Int64
	max   atomic.Int64
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

// flakyStore counts lease calls and misbehaves on demand.
type flakyStore struct {
	store.Store
	tr   *tracker
	fail atomic.Bool
	lost atomic.Bool // report another holder, as if it had taken over
}

func (f *flakyStore) AcquireLease(ctx context.Context, name, holder string, now time.Time, d time.Duration) (model.Lease, error) {
	f.tr.calls.Add(1)
	defer f.tr.done.Add(1)
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
	st     *flakyStore
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

// start runs a loop whose first earlyReturns lead calls return at once.
func start(t *testing.T, clk *clocktest.Fake, tr *tracker, path, name string, earlyReturns int) *node {
	t.Helper()
	n := &node{st: &flakyStore{Store: openDB(t, path), tr: tr}, done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	t.Cleanup(cancel)
	go func() {
		n.done <- lease.Run(ctx, n.st, clk, leaseKey, name, ttl, func(lctx context.Context, epoch int64) {
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
		})
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

// advance moves the clock by d and waits until wantCalls more AcquireLease
// calls have returned (so that no store call is in flight when the next
// advance happens). The loops anchor their ticks to absolute times, so it
// does not matter whether they are already blocked on the clock.
func advance(t *testing.T, clk *clocktest.Fake, tr *tracker, d time.Duration, wantCalls int) {
	t.Helper()
	before := tr.done.Load()
	clk.Advance(d)
	eventually(t, "acquire attempts", func() bool { return tr.done.Load() >= before+int64(wantCalls) })
	time.Sleep(10 * time.Millisecond) // let the loops handle the results
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
	eventually(t, "first attempts", func() bool { return tr.done.Load() >= 3 })
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 10; i++ {
		advance(t, clk, tr, ttl/3, 3)
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
	eventually(t, "first attempts", func() bool { return tr.done.Load() >= 2 })
	time.Sleep(10 * time.Millisecond)
	if b.leading() {
		t.Fatal("b leads while a holds the lease")
	}
	for i := 0; i < 4 && !b.leading(); i++ {
		advance(t, clk, tr, ttl/3, 2)
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

	advance(t, clk, tr, ttl/3, 1) // t=10: 10s since the last renewal, within ttl/2
	if !a.leading() {
		t.Fatal("fenced too early")
	}
	advance(t, clk, tr, ttl/3, 1) // t=20: the fence point (15s) has passed
	eventually(t, "fence", func() bool { return !a.leading() })

	// The store recovers at t=30. The lease expired at t=30 (held_until <= now),
	// so a re-takes it with a new epoch and leads again.
	a.st.fail.Store(false)
	advance(t, clk, tr, ttl/3, 1)
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
	advance(t, clk, tr, ttl/3, 1) // well inside ttl/2: only the loss can stop it
	eventually(t, "a loses lead", func() bool { return !a.leading() })
}

func TestLeadReturningEarlyIsRestarted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	tr := &tracker{}
	a := start(t, clk, tr, path, "a", 1) // the first lead call returns at once
	eventually(t, "first lead", func() bool { return len(a.starts()) == 1 })
	eventually(t, "first attempt", func() bool { return tr.done.Load() >= 1 })
	advance(t, clk, tr, ttl/3, 1)
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
		advance(t, clk, tr, ttl/3, 1)
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
	honourCtx  bool
	entered    chan struct{}
	unblock    chan struct{}
	ctxWasDone atomic.Bool
}

func (h *hangStore) AcquireLease(ctx context.Context, name, holder string, now time.Time, d time.Duration) (model.Lease, error) {
	if h.hang.Load() {
		select {
		case h.entered <- struct{}{}:
		default:
		}
		if h.honourCtx {
			select {
			case <-ctx.Done():
				h.ctxWasDone.Store(true)
				return model.Lease{}, ctx.Err()
			case <-h.unblock:
			}
		} else {
			<-h.unblock
		}
		return model.Lease{}, errors.New("timeout")
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
