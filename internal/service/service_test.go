package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	_ "github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

type fakeWaker struct{ n atomic.Int32 }

func (w *fakeWaker) Wake() { w.n.Add(1) }

type env struct {
	t     *testing.T
	clk   *clocktest.Fake
	url   string
	st    store.Store
	svc   *Svc
	waker *fakeWaker
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newEnv(t *testing.T) *env {
	t.Helper()
	u := url.URL{Scheme: "sqlite", Path: filepath.Join(t.TempDir(), "todo.db")}
	clk := clocktest.New(t0)
	e := &env{t: t, clk: clk, url: u.String(), waker: &fakeWaker{}}
	e.st, e.svc = e.node("A")
	e.svc.SetWaker(e.waker)
	return e
}

// node opens another handle on the same database and wraps it in a Svc.
func (e *env) node(id string) (store.Store, *Svc) {
	e.t.Helper()
	st, err := store.Open(context.Background(), e.url)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = st.Close() })
	svc := New(st, e.clk, Config{NodeID: id, DefaultHour: 9})
	svc.SetLogger(quiet())
	return st, svc
}

var (
	ctx   = context.Background()
	alice = Actor{UserID: 7, ChatID: 7, ChatKind: model.ChatPrivate}
)

func (e *env) must(err error) {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
}

func dayPtr(y, m, d, h int) *time.Time {
	v := time.Date(y, time.Month(m), d, h, 0, 0, 0, time.UTC)
	return &v
}

func at(h, m int) *time.Time {
	v := time.Date(2026, 10, 10, h, m, 0, 0, time.UTC)
	return &v
}

func (e *env) add(a Actor, text string, due *time.Time, rule *recur.Rule) model.Todo {
	e.t.Helper()
	td, err := e.svc.AddTodo(ctx, a, nlparse.Result{Text: text, Due: due, Rule: rule, HasTime: due != nil})
	e.must(err)
	return td
}

func mustRule(t *testing.T, s string) *recur.Rule {
	t.Helper()
	r, err := recur.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

func (e *env) claim(s *Svc, now time.Time) []model.DueReminder {
	e.t.Helper()
	rs, err := s.ClaimReminders(ctx, now, 50)
	e.must(err)
	return rs
}

func TestPrefsCreatesUserWithLocaleDefaults(t *testing.T) {
	e := newEnv(t)
	p, err := e.svc.Prefs(ctx, 1, "fa-IR")
	e.must(err)
	if p.Locale != (i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}) || p.TZ.String() != "UTC" {
		t.Fatalf("fa prefs = %+v", p)
	}
	p, err = e.svc.Prefs(ctx, 2, "de")
	e.must(err)
	if p.Locale != (i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}) {
		t.Fatalf("en prefs = %+v", p)
	}
	// later contact does not reset
	e.must(e.svc.SetLang(ctx, 1, i18n.En))
	e.must(e.svc.SetCalendar(ctx, 1, calendar.Gregorian))
	p, _ = e.svc.Prefs(ctx, 1, "fa")
	if p.Locale.Lang != i18n.En || p.Locale.Cal != calendar.Gregorian {
		t.Fatalf("prefs reset: %+v", p)
	}
	if err := e.svc.SetLang(ctx, 1, "de"); !errors.Is(err, ErrBadLang) {
		t.Fatalf("SetLang = %v", err)
	}
	if err := e.svc.SetCalendar(ctx, 1, "hijri"); !errors.Is(err, ErrBadCalendar) {
		t.Fatalf("SetCalendar = %v", err)
	}
}

func TestAddTodoCreatesReminder(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "buy milk", at(18, 0), nil)
	if td.Num != 1 || td.Status != model.TodoOpen || td.CreatedBy != 7 {
		t.Fatalf("todo = %+v", td)
	}
	if e.waker.n.Load() == 0 {
		t.Fatal("no Wake after adding a todo with a due time")
	}
	e.clk.Set(at(18, 0).Add(time.Second).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	if len(rs) != 1 || rs[0].TodoID != td.ID || rs[0].Kind != model.ReminderDue || !rs[0].FireAt.Equal(*at(18, 0)) {
		t.Fatalf("claimed = %+v", rs)
	}
}

func TestAddTodoWithoutDueMakesNoReminder(t *testing.T) {
	e := newEnv(t)
	e.add(alice, "someday", nil, nil)
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("reminder created for a todo without due")
	}
	if e.waker.n.Load() != 0 {
		t.Fatal("Wake without a schedule change")
	}
}

func TestAddTodoPastDueIsOverdueWithoutReminder(t *testing.T) {
	e := newEnv(t)
	// "today" after the default slot: 23:59 passed? Here: explicit past time.
	td := e.add(alice, "late", at(9, 0), nil)
	if td.DueAt == nil || !td.DueAt.Equal(*at(9, 0)) {
		t.Fatalf("due not stored: %+v", td)
	}
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("reminder created for a past due time")
	}
	// the parser handoff: a date with no time, today, default slot passed
	p := nlparse.Parser{DefaultHour: 9}
	res, err := p.Parse("pay rent today", t0, calendar.Gregorian)
	e.must(err)
	td2, err := e.svc.AddTodo(ctx, alice, res)
	e.must(err)
	if td2.DueAt == nil {
		t.Fatal("due not stored")
	}
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("reminder created for today-after-default-slot")
	}
}

func TestRecurringReminderAdvancesExactlyOnce(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	e.clk.Set(at(18, 0).Add(time.Minute).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	if len(rs) != 1 {
		t.Fatalf("claimed %d", len(rs))
	}
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 55, e.clk.Now()))
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 55, e.clk.Now())) // second call: no-op

	got, err := e.svc.Todo(ctx, alice, td.ID)
	e.must(err)
	want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if got.DueAt == nil || !got.DueAt.Equal(want) {
		t.Fatalf("due = %v, want %v", got.DueAt, want)
	}
	next, ok, err := e.svc.NextWakeup(ctx)
	e.must(err)
	if !ok || !next.Equal(want) {
		t.Fatalf("next wakeup = %v %v", next, ok)
	}
	// exactly one pending reminder: the one for tomorrow
	e.clk.Set(want)
	rs = e.claim(e.svc, e.clk.Now())
	if len(rs) != 1 || !rs[0].FireAt.Equal(want) {
		t.Fatalf("pending after double ReminderSent = %+v", rs)
	}
}

func TestMissedOccurrencesCollapse(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	e.clk.Set(time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)) // 4 days late
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 1, e.clk.Now()))
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	want := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	if !got.DueAt.Equal(want) {
		t.Fatalf("due = %v, want %v", got.DueAt, want)
	}
}

func TestDoneAfterFiredDoesNotSkipNextOccurrence(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	e.clk.Set(at(18, 0).Add(time.Minute).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 55, e.clk.Now()))

	e.clk.Advance(5 * time.Minute)
	got, err := e.svc.AckReminder(ctx, alice, rs[0].ID)
	e.must(err)
	want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if got.Status != model.TodoOpen || got.DueAt == nil || !got.DueAt.Equal(want) {
		t.Fatalf("after Ack: %+v", got)
	}
	// the next occurrence still fires
	e.clk.Set(want)
	if rs := e.claim(e.svc, e.clk.Now()); len(rs) != 1 || !rs[0].FireAt.Equal(want) {
		t.Fatalf("next occurrence lost: %+v", rs)
	}
	_ = td
}

func TestAckCompletesOneOff(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "call", at(13, 0), nil)
	e.clk.Set(at(13, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 3, e.clk.Now()))
	got, err := e.svc.AckReminder(ctx, alice, rs[0].ID)
	e.must(err)
	if got.Status != model.TodoDone || got.DoneAt == nil {
		t.Fatalf("not done: %+v", got)
	}
	again, err := e.svc.AckReminder(ctx, alice, rs[0].ID) // idempotent
	e.must(err)
	if again.Status != model.TodoDone {
		t.Fatalf("second ack: %+v", again)
	}
	if _, err := e.svc.AckReminder(ctx, Actor{UserID: 9, ChatID: 9}, rs[0].ID); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("foreign ack = %v", err)
	}
	_ = td
}

func TestCompleteOneOffCancelsReminders(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "call", at(18, 0), nil)
	_, err := e.svc.Remind(ctx, alice, td.ID, *at(17, 0))
	e.must(err)
	got, err := e.svc.Complete(ctx, alice, td.ID)
	e.must(err)
	if got.Status != model.TodoDone {
		t.Fatalf("status = %s", got.Status)
	}
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("reminders survived Complete")
	}
}

func TestCompleteRecurringTwelveHourRule(t *testing.T) {
	e := newEnv(t)
	// due in 10h: within the window -> advances
	near := e.add(alice, "near", at(22, 0), mustRule(t, "daily 22:00"))
	got, err := e.svc.Complete(ctx, alice, near.ID)
	e.must(err)
	want := time.Date(2026, 10, 11, 22, 0, 0, 0, time.UTC)
	if got.Status != model.TodoOpen || !got.DueAt.Equal(want) {
		t.Fatalf("near: %+v want due %v", got, want)
	}
	// the old reminder is replaced by exactly one for the new due
	e.clk.Set(want)
	if rs := e.claim(e.svc, e.clk.Now()); len(rs) != 1 || !rs[0].FireAt.Equal(want) {
		t.Fatalf("near reminders = %+v", rs)
	}

	e.clk.Set(t0)
	// due in 30h: outside the window -> unchanged, still open
	far := e.add(alice, "far", dayPtr(2026, 10, 11, 18), mustRule(t, "daily 18:00"))
	got, err = e.svc.Complete(ctx, alice, far.ID)
	e.must(err)
	if got.Status != model.TodoOpen || !got.DueAt.Equal(*far.DueAt) {
		t.Fatalf("far changed: %+v", got)
	}
}

func TestAddTodoDateWithoutTimeUsesDefaultSlotReminder(t *testing.T) {
	e := newEnv(t)
	res, err := nlparse.Parser{DefaultHour: 9}.Parse("pay rent tomorrow", t0, calendar.Gregorian)
	e.must(err)
	if _, err := e.svc.AddTodo(ctx, alice, res); err != nil {
		t.Fatal(err)
	}
	next, ok, _ := e.svc.NextWakeup(ctx)
	if want := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC); !ok || !next.Equal(want) {
		t.Fatalf("next = %v %v, want %v", next, ok, want)
	}
}
