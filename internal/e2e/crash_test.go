package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// probeLease reads the poller lease. Acquiring as a third party does not
// change a live lease, so while someone holds it this only reports it.
func probeLease(t *testing.T, st store.Store, clk *clocktest.Fake) model.Lease {
	t.Helper()
	l, err := st.AcquireLease(context.Background(), "poller", "probe", clk.Now(), leaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if l.Holder == "probe" {
		t.Fatal("the probe took the lease: nobody held it")
	}
	return l
}

// Scenario 7e: the leader crashes and never releases the lease. B must stay
// out until the lease expires, then win with a higher epoch, and the update
// it is served again is dropped.
func TestTwoNodesCrashedLeaderFailover(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	m := newMockTelegram(t)
	db := dbURL(t.TempDir())
	ctx := context.Background()

	a := startNode(t, m, db, clk, "node-a")
	waitUntil(t, "node A to poll", func() bool { return m.polled(a.token) })
	b := startNode(t, m, db, clk, "node-b")
	m.queueMessage(1, alice, "/add crash item")
	waitUntil(t, "update 1 to be handled", func() bool { return len(m.sent("crash item")) == 1 })

	// A "crashes": stop it, which releases the lease, then put the lease
	// back under a holder that will never renew it.
	a.stop()
	ghost, err := b.st.AcquireLease(ctx, "poller", "node-a:ghost", clk.Now(), leaseTTL)
	if err != nil || ghost.Holder != "node-a:ghost" {
		t.Fatalf("could not plant the ghost lease: %+v, %v", ghost, err)
	}

	// B retries after TTL/3 but the lease is live: it must not poll. (This
	// negative check needs a short real-time wait for B's attempt to finish.)
	clk.Advance(leaseTTL/3 + time.Second)
	time.Sleep(300 * time.Millisecond)
	if m.polled(b.token) {
		t.Fatal("node B polls while a dead leader's lease is still live")
	}

	// Past the expiry B takes over.
	clk.Advance(leaseTTL)
	waitUntil(t, "node B to take over", func() bool { return m.polled(b.token) })
	m.queueMessage(2, alice, "/add after crash")
	waitUntil(t, "update 2 to be handled", func() bool { return len(m.sent("after crash")) == 1 })

	l := probeLease(t, b.st, clk) // B holds a live lease now
	if !strings.HasPrefix(l.Holder, "node-b:") || l.Epoch <= ghost.Epoch {
		t.Errorf("lease = %+v, want held by node B with an epoch above %d", l, ghost.Epoch)
	}
	if got := m.sent("crash item"); len(got) != 1 {
		t.Errorf("update 1 was answered %d times, want once", len(got))
	}
	if n, err := b.st.CountTodos(ctx, model.TodoQuery{ChatID: alice}); err != nil || n != 2 {
		t.Errorf("alice has %d todos (err %v), want 2", n, err)
	}
}
