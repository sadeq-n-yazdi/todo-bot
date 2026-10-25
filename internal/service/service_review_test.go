package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

func utc(y, m, d, h, mi int) time.Time {
	return time.Date(y, time.Month(m), d, h, mi, 0, 0, time.UTC)
}

// runBoth runs f for every service concurrently, released together.
func runBoth(svcs []*Svc, f func(i int, s *Svc)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, s := range svcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f(i, s)
		}()
	}
	close(start)
	wg.Wait()
}

// fireAndSend adds a recurring todo for a user in tz and sends its first reminder.
func (e *env) fireAndSend(tz, rule string, due time.Time) (model.Todo, model.DueReminder) {
	e.t.Helper()
	e.must(e.svc.SetTZ(ctx, 7, tz))
	e.clk.Set(due.Add(-time.Hour))
	td := e.add(alice, "r", &due, mustRule(e.t, rule))
	e.clk.Set(due.Add(time.Minute))
	rs := e.claim(e.svc, e.clk.Now())
	if len(rs) != 1 {
		e.t.Fatalf("claimed %d", len(rs))
	}
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 1, e.clk.Now()))
	return td, rs[0]
}

func TestRecurrenceAdvancesInUsersZone(t *testing.T) {
	t.Run("berlin dst daily", func(t *testing.T) {
		e := newEnv(t)
		td, _ := e.fireAndSend("Europe/Berlin", "daily 09:00", utc(2026, 10, 24, 7, 0))
		got, _ := e.svc.Todo(ctx, alice, td.ID)
		if want := utc(2026, 10, 25, 8, 0); !got.DueAt.Equal(want) {
			t.Fatalf("due = %v, want %v", got.DueAt, want)
		}
	})
	t.Run("jalali monthly", func(t *testing.T) {
		e := newEnv(t)
		// 1 Aban 1405 = 2026-10-23 10:00 +03:30
		td, _ := e.fireAndSend("Asia/Tehran", "monthly 1 10:00 jalali", utc(2026, 10, 23, 6, 30))
		got, _ := e.svc.Todo(ctx, alice, td.ID)
		if want := utc(2026, 11, 22, 6, 30); !got.DueAt.Equal(want) { // 1 Azar
			t.Fatalf("due = %v, want %v", got.DueAt, want)
		}
	})
}

func TestCompleteRecurringDropsSnoozes(t *testing.T) {
	e := newEnv(t)
	td, fired := e.fireAndSend("UTC", "daily 18:00", utc(2026, 10, 10, 18, 0))
	_, err := e.svc.Snooze(ctx, alice, fired.ID, ui.Snooze1h)
	e.must(err)
	e.clk.Set(utc(2026, 10, 11, 8, 0)) // next due 18:00 is within 12h
	got, _, err := e.svc.Complete(ctx, alice, td.ID)
	e.must(err)
	if !got.DueAt.Equal(utc(2026, 10, 12, 18, 0)) {
		t.Fatalf("not advanced: %v", got.DueAt)
	}
	for _, r := range e.claim(e.svc, utc(2026, 10, 20, 0, 0)) {
		if r.Kind == model.ReminderSnooze {
			t.Fatalf("snooze of the completed occurrence survived: %+v", r.Reminder)
		}
	}
}

func TestCompleteTwelveHourBoundary(t *testing.T) {
	e := newEnv(t)
	exact := e.add(alice, "exact", dayPtr(2026, 10, 11, 0), mustRule(t, "daily 00:00"))
	over := e.add(alice, "over", func() *time.Time { v := utc(2026, 10, 11, 0, 0).Add(time.Second); return &v }(), mustRule(t, "daily 00:00"))
	got, _, err := e.svc.Complete(ctx, alice, exact.ID)
	e.must(err)
	if !got.DueAt.Equal(utc(2026, 10, 12, 0, 0)) {
		t.Fatalf("exactly now+12h did not advance: %v", got.DueAt)
	}
	got, _, err = e.svc.Complete(ctx, alice, over.ID)
	e.must(err)
	if !got.DueAt.Equal(utc(2026, 10, 11, 0, 0).Add(time.Second)) {
		t.Fatalf("now+12h+1s advanced: %v", got.DueAt)
	}
}

func TestEditRacesDeleteAndAdvance(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	for round := 0; round < 3; round++ {
		td := e.add(alice, "x", nil, nil)
		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = e.svc.Edit(ctx, alice, td.ID, "edited")
					time.Sleep(time.Millisecond)
				}
			}
		}()
		time.Sleep(5 * time.Millisecond)
		e.must(b.Delete(ctx, alice, td.ID))
		close(stop)
		wg.Wait()
		if _, err := e.svc.Todo(ctx, alice, td.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("round %d: edit resurrected a deleted todo: %v", round, err)
		}
	}

	rec := e.add(alice, "pills", dayPtr(2026, 10, 10, 18), mustRule(t, "daily 18:00"))
	e.clk.Set(utc(2026, 10, 10, 18, 1))
	r := e.claim(e.svc, e.clk.Now())
	var mine model.DueReminder
	for _, x := range r {
		if x.TodoID == rec.ID {
			mine = x
		}
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = e.svc.Edit(ctx, alice, rec.ID, "edited")
				time.Sleep(time.Millisecond)
			}
		}
	}()
	time.Sleep(5 * time.Millisecond)
	e.must(b.ReminderSent(ctx, mine.ID, 1, e.clk.Now()))
	close(stop)
	wg.Wait()
	got, _ := e.svc.Todo(ctx, alice, rec.ID)
	if !got.DueAt.Equal(utc(2026, 10, 11, 18, 0)) {
		t.Fatalf("edit rolled back the advance: due %v", got.DueAt)
	}
}

func TestSetLangDoesNotRevertConcurrentSetTZ(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	_, err := e.svc.Prefs(ctx, 7, "")
	e.must(err)
	runBoth([]*Svc{e.svc, b}, func(i int, s *Svc) {
		if i == 0 {
			for n := 0; n < 40; n++ {
				l := i18n.En
				if n%2 == 1 {
					l = i18n.Fa
				}
				if err := s.SetLang(ctx, 7, l); err != nil {
					t.Error(err)
				}
				if err := s.SetCalendar(ctx, 7, calendar.Jalali); err != nil {
					t.Error(err)
				}
			}
			return
		}
		time.Sleep(3 * time.Millisecond)
		if err := s.SetTZ(ctx, 7, "Asia/Tehran"); err != nil {
			t.Error(err)
		}
	})
	p, _ := e.svc.Prefs(ctx, 7, "")
	if p.TZ.String() != "Asia/Tehran" {
		t.Fatalf("tz reverted to %s", p.TZ)
	}
}

func TestDigestDoneUsesDigestByZone(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.SetTZ(ctx, 7, "Asia/Tehran"))
	e.must(e.svc.SetDigest(ctx, alice, "09:00")) // 05:30Z
	e.clk.Set(utc(2026, 10, 11, 5, 30).Add(5 * time.Second))
	cs, err := e.svc.ClaimDigests(ctx, e.clk.Now())
	e.must(err)
	if len(cs) != 1 {
		t.Fatalf("claimed %d", len(cs))
	}
	e.must(e.svc.DigestDone(ctx, cs[0], e.clk.Now()))
	c, _ := e.st.GetChat(ctx, 7)
	if want := utc(2026, 10, 12, 5, 30); c.DigestNextAt == nil || !c.DigestNextAt.Equal(want) {
		t.Fatalf("next = %v, want %v", c.DigestNextAt, want)
	}
}

func TestDigestViewPrivateChatFallsBackToOwner(t *testing.T) {
	e := newEnv(t)
	owner := Actor{UserID: 9, ChatID: 9, ChatKind: model.ChatPrivate}
	_, err := e.svc.Prefs(ctx, 9, "fa")
	e.must(err)
	e.must(e.svc.SetTZ(ctx, 9, "Asia/Tehran"))
	e.add(owner, "x", nil, nil) // creates the chat; no digest is set
	v, err := e.svc.DigestView(ctx, 9, e.clk.Now())
	e.must(err)
	if v.Loc.String() != "Asia/Tehran" || v.Locale.Lang != i18n.Fa || v.Locale.Cal != calendar.Jalali {
		t.Fatalf("view = %v %+v", v.Loc, v.Locale)
	}
}

func TestDateOnlySentinelOnlyForToday(t *testing.T) {
	e := newEnv(t)
	s := New(e.st, e.clk, Config{NodeID: "A", DefaultHour: 23, DefaultMinute: 59})
	s.SetLogger(quiet())
	res, err := nlparse.Parser{DefaultHour: 23, DefaultMinute: 59}.Parse("x tomorrow", t0, calendar.Gregorian)
	e.must(err)
	if res.HasTime {
		t.Fatalf("parser result has a time: %+v", res)
	}
	_, err = s.AddTodo(ctx, alice, res)
	e.must(err)
	next, ok, _ := s.NextWakeup(ctx)
	if want := utc(2026, 10, 11, 23, 59); !ok || !next.Equal(want) {
		t.Fatalf("tomorrow 23:59 got no reminder: %v %v", next, ok)
	}
}

func TestEmptyText(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.AddTodo(ctx, alice, nlparse.Result{Text: "  "}); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("AddTodo = %v", err)
	}
	td := e.add(alice, "x", nil, nil)
	if _, err := e.svc.Edit(ctx, alice, td.ID, " "); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("Edit = %v", err)
	}
}

func TestPermanentFailureStopsRecurringSeries(t *testing.T) {
	e := newEnv(t)
	e.svc.retry = func(int) (time.Duration, bool) { t.Error("policy consulted"); return 0, false }
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	r := e.claimOne(e.svc, *at(18, 0))
	e.must(e.svc.ReminderFailed(ctx, r, e.clk.Now(), true))
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	if !got.DueAt.Equal(*at(18, 0)) {
		t.Fatalf("series advanced after a permanent failure: %v", got.DueAt)
	}
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("something is still scheduled")
	}
}

func TestTwoNodesConcurrentClaimsAreDisjoint(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	for i := 0; i < 8; i++ {
		e.add(alice, "x", at(13, 0), nil)
	}
	e.clk.Set(*at(13, 0))
	var mu sync.Mutex
	seen := map[int64]int{}
	runBoth([]*Svc{e.svc, b}, func(_ int, s *Svc) {
		for {
			rs, err := s.ClaimReminders(ctx, e.clk.Now(), 3)
			if err != nil {
				t.Error(err)
				return
			}
			if len(rs) == 0 {
				return
			}
			mu.Lock()
			for _, r := range rs {
				seen[r.ID]++
			}
			mu.Unlock()
		}
	})
	if len(seen) != 8 {
		t.Fatalf("claimed %d distinct, want 8", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("reminder %d claimed %d times", id, n)
		}
	}
}

func TestTwoNodesConcurrentReminderSentAdvancesOnce(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	e.clk.Set(*at(18, 0))
	r := e.claim(e.svc, e.clk.Now())[0]
	runBoth([]*Svc{e.svc, b}, func(_ int, s *Svc) {
		if err := s.ReminderSent(ctx, r.ID, 1, e.clk.Now()); err != nil {
			t.Error(err)
		}
	})
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	if !got.DueAt.Equal(utc(2026, 10, 11, 18, 0)) {
		t.Fatalf("due = %v", got.DueAt)
	}
	e.clk.Set(utc(2026, 10, 11, 18, 0))
	if rs := e.claim(b, e.clk.Now()); len(rs) != 1 {
		t.Fatalf("pending = %d, want 1", len(rs))
	}
}

func TestWakeOnReminderActions(t *testing.T) {
	e := newEnv(t)
	e.svc.retry = func(int) (time.Duration, bool) { return time.Minute, false }
	e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	e.add(alice, "once", at(18, 0), nil)
	e.clk.Set(utc(2026, 10, 10, 18, 1))
	rs := e.claim(e.svc, e.clk.Now())
	if len(rs) != 2 {
		t.Fatalf("claimed %d", len(rs))
	}
	recurring, oneOff := rs[0], rs[1]
	if recurring.Todo.Recur == "" {
		recurring, oneOff = oneOff, recurring
	}
	steps := []struct {
		name string
		f    func() error
	}{
		{"ReminderSent", func() error { return e.svc.ReminderSent(ctx, recurring.ID, 1, e.clk.Now()) }},
		{"Snooze", func() error { _, err := e.svc.Snooze(ctx, alice, recurring.ID, ui.Snooze10m); return err }},
		{"AckReminder", func() error { _, err := e.svc.AckReminder(ctx, alice, oneOff.ID); return err }},
		{"ReminderFailed", func() error {
			rs := e.claim(e.svc, utc(2026, 10, 10, 18, 20))
			if len(rs) == 0 {
				return errors.New("nothing to fail")
			}
			return e.svc.ReminderFailed(ctx, rs[0].Reminder, e.clk.Now(), false)
		}},
	}
	for _, st := range steps {
		before := e.waker.n.Load()
		e.must(st.f())
		if e.waker.n.Load() == before {
			t.Errorf("%s did not Wake", st.name)
		}
	}
}
