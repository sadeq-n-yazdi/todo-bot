package scheduler_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

// hangSender blocks every Send until its context ends, like a Telegram call
// that never answers. errFn, if set, may fail a Send at once instead.
type hangSender struct {
	*tgtest.FakeSender
	mu        sync.Mutex
	deadlines []time.Time // ctx deadline seen by each Send (zero if none)
	chats     []int64
	errFn     func(chatID int64) error
}

func (h *hangSender) Send(ctx context.Context, chatID int64, _ tg.OutMessage) (int, error) {
	dl, _ := ctx.Deadline()
	h.mu.Lock()
	h.deadlines = append(h.deadlines, dl)
	h.chats = append(h.chats, chatID)
	fn := h.errFn
	h.mu.Unlock()
	if fn != nil {
		if err := fn(chatID); err != nil {
			return 0, err
		}
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

func newHangEnv(ttl time.Duration) (*env, *hangSender) {
	clk := clocktest.New(t0)
	b := newFakeBackend(clk)
	b.claimTTL = ttl
	h := &hangSender{FakeSender: &tgtest.FakeSender{}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &env{clk: clk, b: b, snd: h.FakeSender, s: scheduler.New(b, h, clk, log, scheduler.WithClaimTTL(ttl))}, h
}

// tickWithin runs Tick and fails if it takes longer than limit of real time.
// It returns how long Tick took.
func tickWithin(t *testing.T, e *env, limit time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() { defer close(done); e.s.Tick(context.Background()) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("Tick still blocked after %v: a hanging send is not cut off", limit)
	}
	return time.Since(start)
}

func TestSendDeadlineIsClaimTTLMinusMargin(t *testing.T) {
	const ttl = 2 * time.Minute
	e, h := newHangEnv(ttl)
	e.b.addReminder(1, t0, model.TodoOpen)
	h.errFn = func(int64) error { return errBoom } // fail at once; we only read the deadline
	before := time.Now()
	e.s.Tick(context.Background())
	if len(h.deadlines) != 1 || h.deadlines[0].IsZero() {
		t.Fatalf("Send saw no deadline: %v", h.deadlines)
	}
	rem := h.deadlines[0].Sub(before)
	if rem >= ttl || rem < ttl-ttl/4-time.Second {
		t.Errorf("send deadline %v after claim; want in [ttl-ttl/4, ttl) with ttl=%v", rem, ttl)
	}
}

func TestHangingReminderSendIsCutOffBeforeClaimExpires(t *testing.T) {
	const ttl = 800 * time.Millisecond
	e, h := newHangEnv(ttl)
	e.b.addReminder(1, t0, model.TodoOpen)
	if took := tickWithin(t, e, 5*time.Second); took >= ttl {
		t.Fatalf("send ran %v, claim lasts %v", took, ttl)
	}
	if e.b.sentCount(1) != 0 {
		t.Error("a cut-off send must not be marked sent")
	}
	if got := e.b.failed[1]; len(got) != 1 || got[0] {
		t.Errorf("want one transient ReminderFailed, got %v", got)
	}
	if len(h.chats) != 1 {
		t.Errorf("sends = %v, want exactly one (no retry after the deadline)", h.chats)
	}
}

func TestHangingDigestSendIsCutOffAndKeepsClaim(t *testing.T) {
	const ttl = 800 * time.Millisecond
	e, _ := newHangEnv(ttl)
	e.b.chats = []model.Chat{digestChat(500, t0)}
	e.b.views[500] = model.DigestView{ChatID: 500, Loc: time.UTC, NoDateCount: 1}
	if took := tickWithin(t, e, 5*time.Second); took >= ttl {
		t.Fatalf("send ran %v, claim lasts %v", took, ttl)
	}
	if len(e.b.digestsDone) != 0 {
		t.Errorf("DigestDone after a cut-off send: %v", e.b.digestsDone)
	}
}

func TestFallbackSendSharesTheDMDeadline(t *testing.T) {
	const ttl = 800 * time.Millisecond
	e, h := newHangEnv(ttl)
	e.b.addReminder(1, t0, model.TodoOpen)
	e.b.rows[0].dr.Todo.AssigneeID = 777
	e.b.rows[0].dr.Todo.AssigneeName = "bob"
	// The DM fails for good at once; the fallback to the creating chat hangs.
	h.errFn = func(chatID int64) error {
		if chatID == 777 {
			return &tg.APIError{Code: 403, Desc: "blocked"}
		}
		return nil
	}
	if took := tickWithin(t, e, 5*time.Second); took >= ttl {
		t.Fatalf("DM + fallback ran %v, claim lasts %v", took, ttl)
	}
	if len(h.chats) != 2 || h.chats[0] != 777 {
		t.Fatalf("sends = %v, want DM then fallback", h.chats)
	}
	if !h.deadlines[0].Equal(h.deadlines[1]) {
		t.Errorf("DM and fallback must share one deadline: %v vs %v", h.deadlines[0], h.deadlines[1])
	}
}
