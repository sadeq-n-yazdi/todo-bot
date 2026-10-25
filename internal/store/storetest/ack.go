package storetest

import (
	"context"
	"sync"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// ---- Done on a reminder message (service.AckReminder, #157) ----

// sentReminder inserts a reminder and marks it sent.
func sentReminder(t *testing.T, d store.Store, kind model.ReminderKind) model.Reminder {
	t.Helper()
	r := reminder(t, d, 1, kind)
	if ok, err := d.MarkReminderSent(context.Background(), r.ID, 42, *tp(1)); err != nil || !ok {
		t.Fatalf("mark sent: ok=%v err=%v", ok, err)
	}
	return getReminder(t, d, r.ID)
}

func testMarkReminderAcked(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	r := sentReminder(t, d, model.ReminderDue)
	if ok, err := d.MarkReminderAcked(ctx, r.ID, *tp(2)); err != nil || !ok {
		t.Fatalf("first ack: ok=%v err=%v", ok, err)
	}
	if ok, err := d.MarkReminderAcked(ctx, r.ID, *tp(3)); err != nil || ok {
		t.Fatalf("second ack: ok=%v err=%v", ok, err)
	}
	// The mark does not show in the reminder.
	g := getReminder(t, d, r.ID)
	if g.State != r.State || g.MessageID != r.MessageID || g.Attempts != r.Attempts ||
		g.SentAt == nil || !g.SentAt.Equal(*r.SentAt) || !g.FireAt.Equal(r.FireAt) || g.ClaimToken != "" {
		t.Fatalf("ack changed the reminder:\n got %+v\nwant %+v", g, r)
	}
	// The scheduler does not see an acked reminder.
	if _, ok, err := d.NextReminderAt(ctx); err != nil || ok {
		t.Fatalf("next after ack: ok=%v err=%v", ok, err)
	}
	// UpdateReminder on an acked reminder does not clear the mark.
	if err := d.UpdateReminder(ctx, g); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.MarkReminderAcked(ctx, r.ID, *tp(4)); err != nil || ok {
		t.Fatalf("ack after UpdateReminder: ok=%v err=%v", ok, err)
	}

	// Pending, cancelled and missing reminders are not acked, and a failed
	// ack does not change a pending reminder: it is still claimable.
	p := reminder(t, d, 5, model.ReminderCustom)
	if ok, err := d.MarkReminderAcked(ctx, p.ID, *tp(2)); err != nil || ok {
		t.Fatalf("pending: ok=%v err=%v", ok, err)
	}
	got, err := d.ClaimReminders(ctx, "tok", *tp(5), tp(5).Add(claimTTL), 10)
	if err != nil || len(got) != 1 || got[0].ID != p.ID {
		t.Fatalf("claim after failed ack = %v, %v", ids(got), err)
	}
	if err := d.CancelPending(ctx, p.TodoID); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.MarkReminderAcked(ctx, p.ID, *tp(2)); err != nil || ok {
		t.Fatalf("cancelled: ok=%v err=%v", ok, err)
	}
	if ok, err := d.MarkReminderAcked(ctx, 99999, *tp(2)); err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}

// Of several handles that ack one sent reminder at the same time, exactly
// one gets ok=true.
func testMarkReminderAckedConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	r := sentReminder(t, hs[0], model.ReminderDue)
	var mu sync.Mutex
	wins := 0
	parallel(8, func(i int) {
		ok, err := hs[i%len(hs)].MarkReminderAcked(ctx, r.ID, *tp(2 + i))
		if err != nil {
			t.Error(err)
		}
		if ok {
			mu.Lock()
			wins++
			mu.Unlock()
		}
	})
	if wins != 1 {
		t.Fatalf("%d winners, want 1", wins)
	}
	if ok, err := hs[1].MarkReminderAcked(ctx, r.ID, *tp(20)); err != nil || ok {
		t.Fatalf("after race: ok=%v err=%v", ok, err)
	}
}
