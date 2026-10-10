package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/lease"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgbot"
)

const (
	// A long lease keeps the lease loops quiet while the fake clock jumps by
	// minutes: renewals are due every leaseTTL/3 = 20m and a node fences
	// itself after leaseTTL/2 = 30m without a renewal.
	leaseTTL = time.Hour
	claimTTL = 2 * time.Minute
)

// node is a full bot process: a stack that sends through the real tgbot
// adapter, and a lease loop that polls only while the node holds the lease.
// It mirrors runBot in cmd/todo-bot/run.go.
type node struct {
	*stack
	token  string
	bot    *tgbot.Bot
	cancel context.CancelFunc
	done   chan struct{}
}

func startNode(t *testing.T, m *mockTelegram, db string, clk *clocktest.Fake, name string) *node {
	t.Helper()
	token := "tok-" + name
	bot, err := tgbot.New(context.Background(), token, tgbot.Options{ServerURL: m.srv.URL})
	if err != nil {
		t.Fatalf("tgbot.New: %v", err)
	}
	if bot.Username() != botName {
		t.Fatalf("bot username %q", bot.Username())
	}
	n := &node{
		stack: newStack(t, db, clk, stackOpts{node: name, claimTTL: claimTTL, sender: bot}),
		token: token, bot: bot, done: make(chan struct{}),
	}
	lead := func(ctx context.Context, _ int64) {
		if err := bot.DeleteWebhook(ctx); err != nil {
			return
		}
		_ = bot.Poller().Run(ctx, n.h.Handle)
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	go func() {
		defer close(n.done)
		if err := lease.Run(ctx, n.st, clk, "poller", name, leaseTTL, lead); err != nil {
			t.Errorf("node %s: release lease: %v", name, err)
		}
	}()
	t.Cleanup(n.stop) // runs before the stack closes the database
	return n
}

// stop ends the lease loop (which stops polling and releases the lease).
func (n *node) stop() {
	n.cancel()
	<-n.done
}

// tickAll runs the schedulers of all nodes at the same time.
func tickAll(nodes ...*node) {
	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.tick()
		}()
	}
	wg.Wait()
}

func reminderSends(m *mockTelegram) []tgCall {
	return m.sent("⏰ ")
}

// Scenario 7a: two nodes share one SQLite file. Only the lease holder polls,
// and when both schedulers run at once every due reminder is delivered once.
func TestTwoNodesOnlyLeaderPollsAndDeliversOnce(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	m := newMockTelegram(t)
	db := dbURL(t.TempDir())

	a := startNode(t, m, db, clk, "node-a")
	waitUntil(t, "node A to poll", func() bool { return m.polled(a.token) })

	waiters := clk.Waiters()
	b := startNode(t, m, db, clk, "node-b")
	// B has lost its first lease attempt once it waits for the next one.
	waitUntil(t, "node B to wait for its next lease attempt", func() bool { return clk.Waiters() > waiters })
	if m.polled(b.token) {
		t.Fatal("node B polls while node A holds the lease")
	}
	// The lease row itself still names A. A probe by a third holder cannot
	// take a live lease; it only reads who has it.
	if l := probeLease(t, a.st, clk); !strings.HasPrefix(l.Holder, "node-a:") {
		t.Fatalf("lease holder %q, want node A", l.Holder)
	}

	const tasks = 8
	for i := 1; i <= tasks; i++ {
		m.queueMessage(int64(i), alice, fmt.Sprintf("/add task%d in 2m", i))
	}
	waitUntil(t, "all tasks to be added", func() bool { return len(m.sent("Added")) == tasks })
	for _, c := range m.sent("Added") {
		if c.Token != a.token {
			t.Errorf("update handled by %s, not by the leader", c.Token)
		}
	}
	if m.polled(b.token) {
		t.Fatal("node B polled")
	}

	// Nothing is due yet; both nodes agree.
	clk.Advance(time.Minute)
	tickAll(a, b)
	if n := len(reminderSends(m)); n != 0 {
		t.Fatalf("%d reminders sent a minute early", n)
	}

	// Both schedulers claim at the same moment.
	clk.Advance(time.Minute)
	tickAll(a, b)
	tickAll(a, b)
	assertEachSentOnce(t, m, tasks)

	// Later ticks, on either node, add nothing.
	clk.Advance(10 * time.Minute) // less than leaseTTL/3: no lease renewal is due
	tickAll(b, a)
	assertEachSentOnce(t, m, tasks)
}

func assertEachSentOnce(t *testing.T, m *mockTelegram, tasks int) {
	t.Helper()
	for i := 1; i <= tasks; i++ {
		name := fmt.Sprintf("task%d", i)
		var got []string
		for _, c := range reminderSends(m) {
			if strings.Contains(c.Text, name) {
				got = append(got, c.Token)
			}
		}
		if len(got) != 1 {
			t.Errorf("%s was delivered %d times (by %v), want once", name, len(got), got)
		}
	}
	if n := len(reminderSends(m)); n != tasks {
		t.Errorf("%d reminders sent in total, want %d", n, tasks)
	}
}

// Scenario 7b: node A claims a batch but its first send is so slow that the
// claim is more than half spent, so it hands the rest back by letting the
// claim lapse. Node B must not take them early, and must take them once the
// claim has expired. Every reminder is delivered exactly once.
func TestTwoNodesClaimExpiresMidBatch(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	m := newMockTelegram(t)
	db := dbURL(t.TempDir())
	a := startNode(t, m, db, clk, "node-a")
	b := startNode(t, m, db, clk, "node-b")

	const tasks = 4
	for i := 1; i <= tasks; i++ {
		a.say(alice, alice, fmt.Sprintf("/add task%d in 1m", i))
	}
	clk.Advance(time.Minute) // all four are due at T

	// The first reminder A sends takes 90s: more than claimTTL/2 of the claim.
	var once sync.Once
	m.mu.Lock()
	m.onSend = func(c tgCall) {
		if strings.HasPrefix(c.Text, "⏰ ") {
			once.Do(func() { clk.Advance(90 * time.Second) })
		}
	}
	m.mu.Unlock()

	a.tick()
	sent := reminderSends(m)
	if len(sent) != 1 || sent[0].Token != a.token {
		t.Fatalf("after A's tick: %+v, want one reminder sent by A", sent)
	}

	// 90s into a 2m claim: the rest is still A's, so B finds nothing.
	b.tick()
	if n := len(reminderSends(m)); n != 1 {
		t.Fatalf("B took %d reminders while A's claim was live", n-1)
	}

	// The claim expires: B delivers the other three; A has nothing left.
	clk.Advance(30 * time.Second)
	b.tick()
	a.tick()
	b.tick()
	assertEachSentOnce(t, m, tasks)
	rest := 0
	for _, c := range reminderSends(m) {
		if c.Token == b.token {
			rest++
		}
	}
	if rest != tasks-1 {
		t.Errorf("B delivered %d reminders, want %d", rest, tasks-1)
	}
}

// Scenario 7c: the leader stops, the other node takes over the lease and is
// served the same update again (Telegram redelivers what was not confirmed).
// MarkUpdate drops it, so it is acted on once.
func TestTwoNodesFailoverReplayedUpdateIsDropped(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	m := newMockTelegram(t)
	db := dbURL(t.TempDir())

	a := startNode(t, m, db, clk, "node-a")
	waitUntil(t, "node A to poll", func() bool { return m.polled(a.token) })
	b := startNode(t, m, db, clk, "node-b")

	m.queueMessage(1, alice, "/add replayed item")
	waitUntil(t, "update 1 to be handled", func() bool { return len(m.sent("replayed item")) == 1 })

	// A leaves and releases the lease. B's next attempt is due 20m after it
	// started; it then wins and polls from offset 0, so it is sent update 1
	// again.
	a.stop()
	if m.polled(b.token) {
		t.Fatal("node B polled before it held the lease")
	}
	clk.Advance(leaseTTL/3 + time.Second)
	waitUntil(t, "node B to take over", func() bool { return m.polled(b.token) })

	// Update 2 is new. Updates are handled in order, one at a time, so once
	// its answer is out the replay of update 1 has been dealt with.
	m.queueMessage(2, alice, "/add after failover")
	waitUntil(t, "update 2 to be handled", func() bool { return len(m.sent("after failover")) == 1 })

	if got := m.sent("replayed item"); len(got) != 1 || got[0].Token != a.token {
		t.Errorf("update 1 was answered %+v, want once, by node A", got)
	}
	if got := m.sent("after failover"); len(got) != 1 || got[0].Token != b.token {
		t.Errorf("update 2 was answered %+v, want once, by node B", got)
	}
	// B was served update 1 again: the library asks for offset 2 only after
	// it received update 1, and B never saw it before the failover.
	if n := m.count(func(c tgCall) bool { return c.Token == b.token && c.Method == "getUpdates" && c.Offset >= 2 }); n == 0 {
		t.Error("node B was not served the replay; the test proves nothing")
	}
	n, err := b.st.CountTodos(context.Background(), model.TodoQuery{ChatID: alice})
	if err != nil || n != 2 {
		t.Errorf("alice has %d todos (err %v), want 2", n, err)
	}
}
