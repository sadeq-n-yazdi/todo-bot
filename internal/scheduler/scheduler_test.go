package scheduler_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

type env struct {
	clk *clocktest.Fake
	b   *fakeBackend
	snd *tgtest.FakeSender
	s   *scheduler.Scheduler
}

func newEnv(opts ...scheduler.Option) *env {
	clk := clocktest.New(t0)
	b := newFakeBackend(clk)
	snd := &tgtest.FakeSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &env{clk: clk, b: b, snd: snd, s: scheduler.New(b, snd, clk, log, opts...)}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// startRun runs the scheduler and returns a stop func.
func (e *env) startRun(t *testing.T) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := e.s.Run(ctx); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Run did not stop after cancel")
		}
	}
}

func TestTickDeliversDueReminder(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0.Add(-time.Second), model.TodoOpen)
	e.b.addReminder(2, t0.Add(time.Hour), model.TodoOpen)
	e.s.Tick(context.Background())

	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != 101 {
		t.Fatalf("messages = %+v", msgs)
	}
	if !strings.Contains(msgs[0].Msg.Text, "todo 1") || strings.Contains(msgs[0].Msg.Text, "late") {
		t.Errorf("text = %q", msgs[0].Msg.Text)
	}
	if e.b.sentCount(1) != 1 || e.b.sentCount(2) != 0 {
		t.Errorf("sent = %v", e.b.sent)
	}
}

func TestTickLateReminderIsMarkedLate(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0.Add(-scheduler.LateAfter-time.Second), model.TodoOpen)
	e.b.addReminder(2, t0.Add(-scheduler.LateAfter), model.TodoOpen)
	e.s.Tick(context.Background())
	msgs := e.snd.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages = %d", len(msgs))
	}
	if len(msgs[0].Msg.Text) <= len("⏰ todo 1") {
		t.Errorf("reminder 1 should carry a late line: %q", msgs[0].Msg.Text)
	}
	if msgs[1].Msg.Text != "⏰ todo 2" {
		t.Errorf("reminder 2 is exactly at the threshold, not late: %q", msgs[1].Msg.Text)
	}
}

func TestTickCancelsReminderOfClosedTodo(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0, model.TodoDone)
	e.b.addReminder(2, t0, model.TodoDeleted)
	e.s.Tick(context.Background())
	if len(e.snd.Messages()) != 0 {
		t.Errorf("nothing should be sent: %+v", e.snd.Messages())
	}
	if len(e.b.cancelled) != 2 {
		t.Errorf("cancelled = %v", e.b.cancelled)
	}
}

func TestTickBatchesOf50(t *testing.T) {
	e := newEnv()
	for i := int64(1); i <= 120; i++ {
		e.b.addReminder(i, t0, model.TodoOpen)
	}
	e.s.Tick(context.Background())
	if n := len(e.snd.Messages()); n != 120 {
		t.Fatalf("sent %d, want 120", n)
	}
	// 50 + 50 + 20: the short third batch ends the loop.
	if c := e.b.claimCount(); c != 3 {
		t.Errorf("claim calls = %d, want 3", c)
	}
}

func TestTwoSchedulersDeliverEachReminderOnce(t *testing.T) {
	clk := clocktest.New(t0)
	b := newFakeBackend(clk)
	snd := &tgtest.FakeSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s1 := scheduler.New(b, snd, clk, log)
	s2 := scheduler.New(b, snd, clk, log)
	for i := int64(1); i <= 200; i++ {
		b.addReminder(i, t0.Add(-time.Minute), model.TodoOpen)
	}
	var wg sync.WaitGroup
	for _, s := range []*scheduler.Scheduler{s1, s2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Tick(context.Background())
		}()
	}
	wg.Wait()

	perChat := map[int64]int{}
	for _, m := range snd.Messages() {
		perChat[m.ChatID]++
	}
	if len(perChat) != 200 {
		t.Errorf("delivered to %d chats, want 200", len(perChat))
	}
	for chat, n := range perChat {
		if n != 1 {
			t.Errorf("chat %d got %d messages, want 1", chat, n)
		}
	}
	for i := int64(1); i <= 200; i++ {
		if b.sentCount(i) != 1 {
			t.Errorf("reminder %d marked sent %d times", i, b.sentCount(i))
		}
	}
}

func TestOneBadRowDoesNotStopTheBatch(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0, model.TodoOpen)
	e.b.addReminder(2, t0, model.TodoOpen)
	e.b.addReminder(3, t0, model.TodoOpen)
	e.b.sentErrFor[1] = errBoom // ReminderSent fails for the first row
	e.snd.ErrFn = func(chatID int64, _ tg.OutMessage) error {
		if chatID == 102 {
			return errBoom
		}
		return nil
	}
	e.s.Tick(context.Background())
	if e.b.sentCount(3) != 1 {
		t.Errorf("reminder 3 should be delivered; sent = %v", e.b.sent)
	}
	if f := e.b.failed[2]; len(f) != 1 || f[0] {
		t.Errorf("reminder 2 failed = %v, want one transient failure", f)
	}
}

func TestSendErrors(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantFailed    []bool
		wantMigration bool
	}{
		{"permanent 403", &tg.APIError{Code: 403, Desc: "Forbidden: bot was blocked by the user"}, []bool{true}, false},
		{"chat not found", &tg.APIError{Code: 400, Desc: "Bad Request: chat not found"}, []bool{true}, false},
		{"server error is transient", &tg.APIError{Code: 502, Desc: "Bad Gateway"}, []bool{false}, false},
		{"plain error is transient", errBoom, []bool{false}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv()
			e.b.addReminder(1, t0, model.TodoOpen)
			e.snd.ErrFn = func(int64, tg.OutMessage) error { return tc.err }
			e.s.Tick(context.Background())
			if got := e.b.failed[1]; len(got) != len(tc.wantFailed) || got[0] != tc.wantFailed[0] {
				t.Errorf("failed = %v, want %v", got, tc.wantFailed)
			}
			if e.b.sentCount(1) != 0 {
				t.Error("must not be marked sent")
			}
		})
	}
}

func TestPrefsErrorFailsTransiently(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0, model.TodoOpen)
	e.b.prefsErr = errBoom
	e.s.Tick(context.Background())
	if f := e.b.failed[1]; len(f) != 1 || f[0] {
		t.Errorf("failed = %v", f)
	}
	if len(e.snd.Messages()) != 0 {
		t.Error("nothing should be sent")
	}
}

func TestMigrateChatRetriesWithNewID(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0, model.TodoOpen)
	e.snd.ErrFn = func(chatID int64, _ tg.OutMessage) error {
		if chatID == 101 {
			return &tg.APIError{Code: 400, Desc: "group chat was upgraded to a supergroup chat", MigrateTo: -1009}
		}
		return nil
	}
	e.s.Tick(context.Background())
	if len(e.b.migrated) != 1 || e.b.migrated[0] != [2]int64{101, -1009} {
		t.Fatalf("migrated = %v", e.b.migrated)
	}
	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != -1009 {
		t.Fatalf("messages = %+v", msgs)
	}
	if e.b.sentCount(1) != 1 {
		t.Error("should be marked sent after the retry")
	}
}

func TestRateLimitShortRetryAfterWaitsInline(t *testing.T) {
	e := newEnv(scheduler.WithClaimTTL(2 * time.Minute))
	e.b.addReminder(1, t0, model.TodoOpen)
	calls := 0
	e.snd.ErrFn = func(int64, tg.OutMessage) error {
		calls++
		if calls == 1 {
			return &tg.APIError{Code: 429, Desc: "Too Many Requests", RetryAfter: 59 * time.Second}
		}
		return nil
	}
	done := make(chan struct{})
	go func() { defer close(done); e.s.Tick(context.Background()) }()

	eventually(t, "tick to wait on the clock", func() bool { return e.clk.Waiters() == 1 })
	if len(e.b.failed[1]) != 0 || e.b.sentCount(1) != 0 {
		t.Fatal("must not fail or send while waiting")
	}
	e.clk.Advance(59 * time.Second)
	<-done
	if e.b.sentCount(1) != 1 || len(e.b.failed[1]) != 0 {
		t.Errorf("sent=%d failed=%v", e.b.sentCount(1), e.b.failed[1])
	}
	if len(e.snd.Messages()) != 1 {
		t.Errorf("messages = %d", len(e.snd.Messages()))
	}
}

func TestRateLimitLongRetryAfterHandsBack(t *testing.T) {
	e := newEnv(scheduler.WithClaimTTL(2 * time.Minute))
	e.b.addReminder(1, t0, model.TodoOpen)
	e.snd.ErrFn = func(int64, tg.OutMessage) error {
		return &tg.APIError{Code: 429, Desc: "Too Many Requests", RetryAfter: 60 * time.Second} // == ttl/2: not shorter
	}
	e.s.Tick(context.Background()) // would block forever if it waited
	if f := e.b.failed[1]; len(f) != 1 || f[0] {
		t.Errorf("failed = %v, want one non-permanent", f)
	}
	if e.clk.Waiters() != 0 {
		t.Error("must not wait inline")
	}
}

func TestRateLimitInlineWaitStopsOnCancel(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0, model.TodoOpen)
	e.snd.ErrFn = func(int64, tg.OutMessage) error {
		return &tg.APIError{Code: 429, RetryAfter: 10 * time.Second}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); e.s.Tick(ctx) }()
	eventually(t, "wait", func() bool { return e.clk.Waiters() == 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Tick did not return after cancel")
	}
}

func digestChat(id int64, next time.Time) model.Chat {
	return model.Chat{ID: id, Kind: model.ChatPrivate, DigestTime: "08:00", DigestBy: 7, DigestNextAt: &next}
}

func TestDigestSentThenDone(t *testing.T) {
	e := newEnv()
	e.b.chats = []model.Chat{digestChat(500, t0.Add(-time.Minute))}
	e.b.views[500] = model.DigestView{ChatID: 500, Loc: time.UTC, Today: []model.Todo{{Text: "buy milk", Status: model.TodoOpen}}}
	e.s.Tick(context.Background())
	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != 500 || !strings.Contains(msgs[0].Msg.Text, "buy milk") {
		t.Fatalf("messages = %+v", msgs)
	}
	if len(e.b.digestsDone) != 1 || e.b.digestsDone[0] != 500 {
		t.Errorf("digestsDone = %v", e.b.digestsDone)
	}
}

func TestDigestEmptyViewNotSentButDone(t *testing.T) {
	e := newEnv()
	e.b.chats = []model.Chat{digestChat(500, t0)}
	e.b.views[500] = model.DigestView{ChatID: 500, Loc: time.UTC}
	e.s.Tick(context.Background())
	if len(e.snd.Messages()) != 0 {
		t.Errorf("empty digest must not be sent: %+v", e.snd.Messages())
	}
	if len(e.b.digestsDone) != 1 {
		t.Errorf("digestsDone = %v", e.b.digestsDone)
	}
}

func TestDigestMoreThanAnHourLateSkippedButDone(t *testing.T) {
	e := newEnv()
	e.b.chats = []model.Chat{
		digestChat(500, t0.Add(-time.Hour-time.Second)),
		digestChat(501, t0.Add(-time.Hour)), // exactly 1h: still sent
	}
	for _, id := range []int64{500, 501} {
		e.b.views[id] = model.DigestView{ChatID: id, Loc: time.UTC, Today: []model.Todo{{Text: "x", Status: model.TodoOpen}}}
	}
	e.s.Tick(context.Background())
	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != 501 {
		t.Fatalf("messages = %+v", msgs)
	}
	if len(e.b.digestsDone) != 2 {
		t.Errorf("digestsDone = %v, want both", e.b.digestsDone)
	}
}

func TestDigestSendFailureStillDone(t *testing.T) {
	e := newEnv()
	e.b.chats = []model.Chat{digestChat(500, t0)}
	e.b.views[500] = model.DigestView{ChatID: 500, Loc: time.UTC, NoDateCount: 2}
	e.snd.ErrFn = func(int64, tg.OutMessage) error { return errBoom }
	e.s.Tick(context.Background())
	if len(e.b.digestsDone) != 1 {
		t.Errorf("digestsDone = %v", e.b.digestsDone)
	}
}

func TestHousekeepAtMostOncePerHour(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	e.s.Tick(ctx)
	e.s.Tick(ctx)
	if e.b.housekeeps != 1 {
		t.Fatalf("housekeeps = %d, want 1", e.b.housekeeps)
	}
	e.clk.Advance(59 * time.Minute)
	e.s.Tick(ctx)
	if e.b.housekeeps != 1 {
		t.Fatalf("housekeeps = %d after 59m, want 1", e.b.housekeeps)
	}
	e.clk.Advance(time.Minute)
	e.s.Tick(ctx)
	if e.b.housekeeps != 2 {
		t.Fatalf("housekeeps = %d after 1h, want 2", e.b.housekeeps)
	}
}

func TestRunTicksImmediatelyAndStopsOnCancel(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0.Add(-time.Hour), model.TodoOpen)
	stop := e.startRun(t)
	eventually(t, "startup tick", func() bool { return e.b.sentCount(1) == 1 })
	stop()
}

func TestRunSleepsUntilNextWakeup(t *testing.T) {
	e := newEnv()
	e.b.addReminder(1, t0.Add(90*time.Second), model.TodoOpen)
	stop := e.startRun(t)
	defer stop()
	eventually(t, "sleep", func() bool { return e.clk.Waiters() == 1 })
	e.clk.Advance(89 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if e.b.sentCount(1) != 0 {
		t.Fatal("delivered early")
	}
	e.clk.Advance(time.Second)
	eventually(t, "delivery at fire time", func() bool { return e.b.sentCount(1) == 1 })
}

func TestRunSleepIsCappedAtMaxSleep(t *testing.T) {
	tests := []struct {
		name string
		opts []scheduler.Option
		cap  time.Duration
	}{
		{"default", nil, scheduler.MaxSleep},
		{"configured", []scheduler.Option{scheduler.WithMaxSleep(30 * time.Second)}, 30 * time.Second},
		{"non-positive keeps default", []scheduler.Option{scheduler.WithMaxSleep(0)}, scheduler.MaxSleep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(tc.opts...)
			e.b.addReminder(1, t0.Add(24*time.Hour), model.TodoOpen) // far away
			stop := e.startRun(t)
			defer stop()
			eventually(t, "first sleep", func() bool { return e.clk.Waiters() == 1 && e.b.claimCount() == 1 })
			e.clk.Advance(tc.cap - time.Second)
			time.Sleep(20 * time.Millisecond)
			if c := e.b.claimCount(); c != 1 {
				t.Fatalf("re-planned too early: %d claims", c)
			}
			e.clk.Advance(time.Second)
			eventually(t, "re-plan at the cap", func() bool { return e.b.claimCount() == 2 })
		})
	}
}

func TestRunSleepsMaxWhenNothingPending(t *testing.T) {
	e := newEnv(scheduler.WithMaxSleep(time.Minute))
	stop := e.startRun(t)
	defer stop()
	eventually(t, "sleep", func() bool { return e.clk.Waiters() == 1 })
	e.clk.Advance(time.Minute)
	eventually(t, "second tick", func() bool { return e.b.claimCount() == 2 })
}

func TestRunOverdueNextWakeupDoesNotSleep(t *testing.T) {
	// A claim held by another node expiring in the past clamps to 0.
	e := newEnv()
	e.b.addReminder(1, t0.Add(-time.Hour), model.TodoOpen)
	e.b.rows[0].claimUntil = t0.Add(30 * time.Second) // claimed elsewhere
	stop := e.startRun(t)
	defer stop()
	eventually(t, "sleep for claim expiry", func() bool { return e.clk.Waiters() == 1 })
	e.clk.Advance(30 * time.Second)
	// The other node never finished; its claim expired, so we deliver.
	eventually(t, "takeover", func() bool { return e.b.sentCount(1) == 1 })
}

func TestWakeReplansEarly(t *testing.T) {
	e := newEnv()
	stop := e.startRun(t)
	defer stop()
	eventually(t, "first sleep", func() bool { return e.clk.Waiters() == 1 && e.b.claimCount() == 1 })

	e.b.addReminder(1, t0, model.TodoOpen) // created while sleeping
	e.s.Wake()
	e.s.Wake() // never blocks, coalesces
	eventually(t, "delivery after Wake", func() bool { return e.b.sentCount(1) == 1 })
}

func TestWakeNeverBlocks(t *testing.T) {
	e := newEnv()
	done := make(chan struct{})
	go func() {
		for range 10 {
			e.s.Wake()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wake blocked")
	}
}

func TestRunStopsWhileSleeping(t *testing.T) {
	e := newEnv()
	stop := e.startRun(t)
	eventually(t, "sleep", func() bool { return e.clk.Waiters() == 1 })
	stop()
}

func TestClaimErrorDoesNotStopRun(t *testing.T) {
	e := newEnv(scheduler.WithMaxSleep(time.Minute))
	e.b.claimErr = errBoom
	stop := e.startRun(t)
	defer stop()
	eventually(t, "sleep", func() bool { return e.clk.Waiters() == 1 })
	e.b.mu.Lock()
	e.b.claimErr = nil
	e.b.mu.Unlock()
	e.b.addReminder(1, t0, model.TodoOpen)
	e.clk.Advance(time.Minute)
	eventually(t, "recovery", func() bool { return e.b.sentCount(1) == 1 })
}
