package web

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

// TestWritesWakeScheduler: each write of the web wakes the scheduler one
// time.
func TestWritesWakeScheduler(t *testing.T) {
	e := newWriteEnv(t)
	cw := &countWaker{}
	e.svc.SetWaker(cw)
	wakes := func(name string, f func()) {
		t.Helper()
		before := cw.n.Load()
		f()
		if got := cw.n.Load() - before; got != 1 {
			t.Errorf("%s: %d wakes, want 1", name, got)
		}
	}
	var it itemDTO
	wakes("add", func() { it = e.openItem(e.ann, idAnn, "call the bank tomorrow 10:00") })
	wakes("remind", func() {
		wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/remind", it.ID)), `{"preset":"1h"}`, bearer(e.ann)), 201)
	})
	wakes("done", func() {
		wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann)), 200)
	})
	it = e.openItem(e.ann, idAnn, "x")
	wakes("delete", func() {
		wantStatus(t, e.do("DELETE", chatPath(idAnn, fmt.Sprintf("/items/%d", it.ID)), "", bearer(e.ann)), 204)
	})
}

// TestWebReminderReachesScheduler runs a real scheduler that sleeps up to a
// day. A reminder that the web makes for 10 minutes later arrives after 10
// minutes of fake time. Without the wake, the scheduler sleeps past it.
func TestWebReminderReachesScheduler(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(e *writeEnv) // makes a reminder 10 minutes from now
	}{
		{"add", func(e *writeEnv) { e.openItem(e.ann, idAnn, "tea in 10 minutes") }},
		{"remind", func(e *writeEnv) {
			it := e.openItem(e.ann, idAnn, "tea")
			wantStatus(e.t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/remind", it.ID)), `{"preset":"10m"}`, bearer(e.ann)), 201)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newWriteEnv(t)
			// The fixture inserts no reminders: only those of the web exist.
			out := &tgtest.FakeSender{}
			sc := scheduler.New(e.svc, out, e.clk, slog.New(slog.DiscardHandler), scheduler.WithMaxSleep(24*time.Hour))
			e.svc.SetWaker(sc)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); _ = sc.Run(ctx) }()
			t.Cleanup(func() { cancel(); <-done })

			waitFor(t, func() bool { return e.clk.Waiters() == 1 })
			tc.write(e)
			// The wake makes the scheduler plan again: a second waiter.
			waitFor(t, func() bool { return e.clk.Waiters() == 2 })
			if n := len(out.Messages()); n != 0 {
				t.Fatalf("%d messages before the time", n)
			}
			e.clk.Advance(10 * time.Minute)
			waitFor(t, func() bool { return len(out.Messages()) == 1 })
			if m := out.Messages()[0]; m.ChatID != idAnn || !strings.Contains(m.Msg.Text, "tea") {
				t.Fatalf("reminder %+v", m)
			}
		})
	}
}
