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

// flakyStore fails lease calls while fail is set.
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

func start(t *testing.T, clk *clocktest.Fake, path, name string) *node {
	t.Helper()
	n := &node{st: &flakyStore{Store: openDB(t, path)}, done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	t.Cleanup(cancel)
	go func() {
		n.done <- lease.Run(ctx, n.st, clk, leaseKey, name, ttl, func(lctx context.Context, epoch int64) {
			n.mu.Lock()
			n.epochs = append(n.epochs, epoch)
			n.running++
			n.mu.Unlock()
			<-lctx.Done()
			n.mu.Lock()
			n.running--
			n.mu.Unlock()
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

// tick waits until the loops are blocked on the clock, then advances it.
func tick(t *testing.T, clk *clocktest.Fake, waiters int, d time.Duration) {
	t.Helper()
	eventually(t, "loops to wait on the clock", func() bool { return clk.Waiters() >= waiters })
	clk.Advance(d)
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
	a := start(t, clk, path, "a")
	b := start(t, clk, path, "b")
	c := start(t, clk, path, "a") // same NodeID as a: must still not co-lead
	nodes := []*node{a, b, c}
	eventually(t, "a leader", func() bool { return countLeaders(nodes...) == 1 })
	eventually(t, "all loops waiting", func() bool { return clk.Waiters() >= 3 })
	for i := 0; i < 10; i++ {
		tick(t, clk, 3, ttl/3)
		time.Sleep(3 * time.Millisecond)
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
}

func TestTakeoverAfterTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	a := start(t, clk, path, "a")
	eventually(t, "a leads", a.leading)
	a.st.fail.Store(true) // a can neither renew nor release
	b := start(t, clk, path, "b")
	eventually(t, "both waiting", func() bool { return clk.Waiters() >= 2 })
	if b.leading() {
		t.Fatal("b leads while a holds the lease")
	}
	for i := 0; i < 4 && !b.leading(); i++ {
		tick(t, clk, 2, ttl/3)
		time.Sleep(3 * time.Millisecond)
	}
	eventually(t, "b takes over", b.leading)
	eventually(t, "a stops", func() bool { return !a.leading() })
	if got := b.starts(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("b epochs = %v, want [2]", got)
	}
}

func TestFenceAfterHalfTTLWithoutRenewal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	a := start(t, clk, path, "a")
	eventually(t, "a leads", a.leading)
	a.st.fail.Store(true)

	tick(t, clk, 1, ttl/3) // 10s since the last renewal: within ttl/2
	time.Sleep(5 * time.Millisecond)
	if !a.leading() {
		t.Fatal("fenced too early")
	}
	tick(t, clk, 1, ttl/3) // 20s >= 15s
	eventually(t, "fence", func() bool { return !a.leading() })

	// The store recovers while the lease is still a's own: it leads again.
	a.st.fail.Store(false)
	tick(t, clk, 1, ttl/3)
	eventually(t, "re-lead", a.leading)
}

func TestLeadContextCancelledWhenLost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	a := start(t, clk, path, "a")
	eventually(t, "a leads", a.leading)
	a.st.lost.Store(true)
	tick(t, clk, 1, ttl/3) // well inside ttl/2: only the loss can stop it
	eventually(t, "a loses lead", func() bool { return !a.leading() })
}

func TestShutdownReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	clk := clocktest.New(t0)
	a := start(t, clk, path, "a")
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
	a := start(t, clk, path, "a")
	eventually(t, "a leads", a.leading)
	other := openDB(t, path)
	for i := 0; i < 3; i++ {
		tick(t, clk, 1, ttl/3)
		want := clk.Now().Add(ttl)
		eventually(t, "renewal", func() bool {
			// A probe far in the past never wins, so it only reads the row.
			l, err := other.AcquireLease(context.Background(), leaseKey, "probe:1", t0.Add(-time.Hour), ttl)
			return err == nil && l.Until.Equal(want)
		})
	}
}
