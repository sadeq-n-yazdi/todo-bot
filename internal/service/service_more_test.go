package service

import (
	"errors"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

func TestSnooze(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.SetTZ(ctx, 7, "Europe/Berlin"))
	td := e.add(alice, "call", at(12, 30), nil)
	e.clk.Set(at(12, 31).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 1, e.clk.Now()))
	e.waker.n.Store(0)

	now := e.clk.Now()
	tomorrow := time.Date(2026, 10, 11, 9, 0, 0, 0, time.FixedZone("CEST", 2*3600)).UTC()
	for _, c := range []struct {
		arg  string
		want time.Time
	}{
		{ui.Snooze10m, now.Add(10 * time.Minute)},
		{ui.Snooze1h, now.Add(time.Hour)},
		{ui.SnoozeTomorrow, tomorrow},
	} {
		r, err := e.svc.Snooze(ctx, alice, rs[0].ID, c.arg)
		e.must(err)
		if r.Kind != model.ReminderSnooze || r.State != model.ReminderPending || !r.FireAt.Equal(c.want) || r.TodoID != td.ID {
			t.Errorf("snooze %s = %+v, want fire %v", c.arg, r, c.want)
		}
	}
	if e.waker.n.Load() != 3 {
		t.Errorf("Wake calls = %d, want 3", e.waker.n.Load())
	}
	if _, err := e.svc.Snooze(ctx, alice, rs[0].ID, "5m"); err == nil {
		t.Error("unknown snooze arg accepted")
	}
	if _, err := e.svc.Snooze(ctx, Actor{UserID: 9, ChatID: 9}, rs[0].ID, ui.Snooze10m); !errors.Is(err, ErrForeignChat) {
		t.Errorf("foreign snooze = %v", err)
	}
}

func TestSnoozeTomorrowAcrossDST(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.SetTZ(ctx, 7, "Europe/Berlin"))
	td := e.add(alice, "x", at(12, 30), nil)
	// 2026-10-24 21:00 UTC is 23:00 CEST; tomorrow 09:00 is 25 hours ahead
	// of local midnight-to-midnight because clocks go back on 10-25.
	e.clk.Set(time.Date(2026, 10, 24, 21, 0, 0, 0, time.UTC))
	r := e.mustRemind(td.ID, time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC))
	got, err := e.svc.Snooze(ctx, alice, r.ID, ui.SnoozeTomorrow)
	e.must(err)
	want := time.Date(2026, 10, 25, 9, 0, 0, 0, time.FixedZone("CET", 3600)).UTC()
	if !got.FireAt.Equal(want) {
		t.Fatalf("fire = %v, want %v", got.FireAt, want)
	}
}

func (e *env) mustRemind(todoID int64, when time.Time) model.Reminder {
	e.t.Helper()
	r, err := e.svc.Remind(ctx, alice, todoID, when)
	e.must(err)
	return r
}

func TestRemind(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "x", nil, nil)
	if _, err := e.svc.Remind(ctx, alice, td.ID, t0); !errors.Is(err, ErrInPast) {
		t.Fatalf("remind now = %v", err)
	}
	r := e.mustRemind(td.ID, t0.Add(time.Hour))
	if r.Kind != model.ReminderCustom || r.ChatID != 7 {
		t.Fatalf("reminder = %+v", r)
	}
	if _, err := e.svc.Remind(ctx, Actor{UserID: 9, ChatID: 9}, td.ID, t0.Add(time.Hour)); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("foreign = %v", err)
	}
	if _, err := e.svc.Remind(ctx, alice, 999, t0.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestSetTZReanchorsRecurringAndDigests(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 09:00"))
	oneOff := e.add(alice, "once", at(20, 0), nil)
	e.must(e.svc.SetDigest(ctx, alice, "08:00"))
	chat, _ := e.st.GetChat(ctx, 7)
	if want := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC); !chat.DigestNextAt.Equal(want) {
		t.Fatalf("digest next = %v", chat.DigestNextAt)
	}
	e.waker.n.Store(0)

	if err := e.svc.SetTZ(ctx, 7, "Mars/Base"); !errors.Is(err, ErrBadTZ) {
		t.Fatalf("bad tz = %v", err)
	}
	e.must(e.svc.SetTZ(ctx, 7, "Asia/Tehran")) // +03:30, no DST since 2022
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	want := time.Date(2026, 10, 11, 5, 30, 0, 0, time.UTC) // 09:00 Tehran
	if got.TZ != "Asia/Tehran" || !got.DueAt.Equal(want) {
		t.Fatalf("recurring = tz %s due %v, want %v", got.TZ, got.DueAt, want)
	}
	if o, _ := e.svc.Todo(ctx, alice, oneOff.ID); !o.DueAt.Equal(*at(20, 0)) {
		t.Fatalf("one-off moved: %v", o.DueAt)
	}
	chat, _ = e.st.GetChat(ctx, 7)
	if wantD := time.Date(2026, 10, 11, 4, 30, 0, 0, time.UTC); !chat.DigestNextAt.Equal(wantD) {
		t.Fatalf("digest next = %v, want %v", chat.DigestNextAt, wantD)
	}
	if e.waker.n.Load() == 0 {
		t.Fatal("no Wake after SetTZ")
	}
	// the old reminder is gone, the new one fires at the new time
	e.clk.Set(time.Date(2026, 10, 10, 19, 0, 0, 0, time.UTC))
	for _, r := range e.claim(e.svc, e.clk.Now()) {
		if r.TodoID == td.ID {
			t.Fatalf("stale reminder of the recurring todo: %+v", r)
		}
	}
	e.clk.Set(want)
	var mine []model.DueReminder
	for _, r := range e.claim(e.svc, e.clk.Now()) {
		if r.TodoID == td.ID {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 || !mine[0].FireAt.Equal(want) {
		t.Fatalf("re-anchored reminder = %+v", mine)
	}
}

func TestSetDigest(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"9", "24:00", "09:60", "9:5", "noon"} {
		if err := e.svc.SetDigest(ctx, alice, bad); !errors.Is(err, ErrBadTime) {
			t.Errorf("SetDigest(%q) = %v", bad, err)
		}
	}
	e.must(e.svc.SetDigest(ctx, alice, "۹:۰۰")) // Persian digits
	c, err := e.st.GetChat(ctx, 7)
	e.must(err)
	if c.DigestTime != "09:00" || c.DigestBy != 7 || c.DigestNextAt == nil || !c.DigestNextAt.Equal(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("chat = %+v", c)
	}
	e.must(e.svc.SetDigest(ctx, alice, ""))
	c, _ = e.st.GetChat(ctx, 7)
	if c.DigestTime != "" || c.DigestNextAt != nil {
		t.Fatalf("not off: %+v", c)
	}
}

func TestDigestViewRules(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.SetTZ(ctx, 7, "Europe/Berlin"))
	e.must(e.svc.SetLang(ctx, 7, i18n.Fa))
	e.must(e.svc.SetCalendar(ctx, 7, calendar.Jalali))
	e.must(e.svc.SetDigest(ctx, alice, "08:00"))
	// now = 2026-10-10 12:00 UTC = 14:00 Berlin
	e.add(alice, "yesterday", dayPtr(2026, 10, 9, 10), nil)
	e.add(alice, "this morning", at(5, 0), nil)   // 07:00 Berlin, today
	e.add(alice, "tonight", at(19, 0), nil)       // today
	e.add(alice, "late tonight", at(22, 30), nil) // 00:30 Berlin tomorrow: not today
	e.add(alice, "someday", nil, nil)
	done := e.add(alice, "finished", at(6, 0), nil)
	_, err := e.svc.Complete(ctx, alice, done.ID)
	e.must(err)

	v, err := e.svc.DigestView(ctx, 7, e.clk.Now())
	e.must(err)
	names := func(ts []model.Todo) (out []string) {
		for _, t := range ts {
			out = append(out, t.Text)
		}
		return
	}
	if got := names(v.Overdue); len(got) != 1 || got[0] != "yesterday" {
		t.Errorf("overdue = %v", got)
	}
	if got := names(v.Today); len(got) != 2 || got[0] != "this morning" || got[1] != "tonight" {
		t.Errorf("today = %v", got)
	}
	if v.NoDateCount != 1 {
		t.Errorf("no-date = %d", v.NoDateCount)
	}
	if v.Locale != (i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}) || v.Loc.String() != "Europe/Berlin" || v.ChatID != 7 {
		t.Errorf("view meta = %+v", v)
	}
}

func TestDigestViewLocaleFollowsDigestBy(t *testing.T) {
	e := newEnv(t)
	group := Actor{UserID: 8, ChatID: -100, ChatKind: model.ChatGroup, ChatTitle: "fam"}
	_, err := e.svc.Prefs(ctx, 8, "fa")
	e.must(err)
	_, err = e.svc.Prefs(ctx, 7, "en")
	e.must(err)
	e.must(e.svc.SetDigest(ctx, group, "07:30"))
	// a different member talks in the group; the digest still uses user 8
	e.add(Actor{UserID: 7, ChatID: -100, ChatKind: model.ChatGroup}, "x", nil, nil)
	v, err := e.svc.DigestView(ctx, -100, e.clk.Now())
	e.must(err)
	if v.Locale.Lang != i18n.Fa {
		t.Fatalf("locale = %+v", v.Locale)
	}
}

func TestListPaging(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 23; i++ {
		e.add(alice, "t", nil, nil)
	}
	for page, wantLen := range []int{10, 10, 3} {
		ts, pages, err := e.svc.List(ctx, 7, page)
		e.must(err)
		if len(ts) != wantLen || pages != 3 {
			t.Errorf("page %d: %d items, %d pages", page, len(ts), pages)
		}
	}
	ts, pages, _ := e.svc.List(ctx, 7, 99)
	if len(ts) != 3 || pages != 3 {
		t.Errorf("clamped page: %d items %d pages", len(ts), pages)
	}
	ts, pages, err := e.svc.List(ctx, 555, 0)
	e.must(err)
	if len(ts) != 0 || pages != 1 {
		t.Errorf("empty list: %d items %d pages", len(ts), pages)
	}
	td, err := e.svc.ByNum(ctx, 7, 5)
	e.must(err)
	if td.Num != 5 {
		t.Errorf("ByNum = %+v", td)
	}
	if _, err := e.svc.ByNum(ctx, 7, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByNum missing = %v", err)
	}
}

func TestDeleteEditAndTodo(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "old", at(18, 0), mustRule(t, "daily 18:00"))
	other := Actor{UserID: 9, ChatID: 9, ChatKind: model.ChatPrivate}
	if _, err := e.svc.Edit(ctx, other, td.ID, "x"); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("foreign edit = %v", err)
	}
	if err := e.svc.Delete(ctx, other, td.ID); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("foreign delete = %v", err)
	}
	got, err := e.svc.Edit(ctx, alice, td.ID, "  new text ")
	e.must(err)
	if got.Text != "new text" {
		t.Fatalf("edit = %q", got.Text)
	}
	e.must(e.svc.Delete(ctx, alice, td.ID))
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("deleting a recurring item left its reminder")
	}
	if _, err := e.svc.Todo(ctx, alice, td.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Todo(deleted) = %v", err)
	}
	if _, err := e.svc.Todo(ctx, alice, 4242); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Todo(missing) = %v", err)
	}
	d := e.add(alice, "done one", nil, nil)
	_, err = e.svc.Complete(ctx, alice, d.ID)
	e.must(err)
	got, err = e.svc.Todo(ctx, alice, d.ID)
	e.must(err)
	if got.Status != model.TodoDone {
		t.Fatalf("Todo(done) = %+v", got)
	}
	if _, err := e.svc.Todo(ctx, other, d.ID); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("Todo(foreign) = %v", err)
	}
}

// failing returns a reminder claimed and ready to be failed.
func (e *env) claimOne(s *Svc, at time.Time) model.Reminder {
	e.t.Helper()
	e.clk.Set(at)
	rs := e.claim(s, at)
	if len(rs) != 1 {
		e.t.Fatalf("claimed %d, want 1", len(rs))
	}
	return rs[0].Reminder
}

func TestReminderFailedBacksOffThenFails(t *testing.T) {
	e := newEnv(t)
	var seen []int
	e.svc.retry = func(attempts int) (time.Duration, bool) {
		seen = append(seen, attempts)
		return time.Duration(attempts) * time.Minute, attempts >= 3
	}
	td := e.add(alice, "x", at(13, 0), nil)

	r := e.claimOne(e.svc, *at(13, 0))
	e.must(e.svc.ReminderFailed(ctx, r, e.clk.Now(), false))
	got, _ := e.st.GetReminder(ctx, r.ID)
	if got.State != model.ReminderPending || got.Attempts != 1 || !got.FireAt.Equal(at(13, 1).UTC()) {
		t.Fatalf("after 1st failure = %+v", got)
	}
	// the claim was released: it can be claimed straight away at the new time
	r = e.claimOne(e.svc, at(13, 1).UTC())
	e.must(e.svc.ReminderFailed(ctx, r, e.clk.Now(), false))
	r = e.claimOne(e.svc, at(13, 3).UTC())
	e.must(e.svc.ReminderFailed(ctx, r, e.clk.Now(), false))
	got, _ = e.st.GetReminder(ctx, r.ID)
	if got.State != model.ReminderFailed || got.Attempts != 3 {
		t.Fatalf("after 3rd failure = %+v", got)
	}
	if len(seen) != 3 || seen[0] != 1 || seen[2] != 3 {
		t.Fatalf("policy saw attempts %v", seen)
	}
	if _, ok, _ := e.svc.NextWakeup(ctx); ok {
		t.Fatal("failed reminder still scheduled")
	}
	_ = td
}

func TestReminderFailedPermanentSkipsPolicy(t *testing.T) {
	e := newEnv(t)
	e.svc.retry = func(int) (time.Duration, bool) { t.Error("policy consulted"); return time.Minute, false }
	e.add(alice, "x", at(13, 0), nil)
	r := e.claimOne(e.svc, *at(13, 0))
	e.must(e.svc.ReminderFailed(ctx, r, e.clk.Now(), true))
	got, _ := e.st.GetReminder(ctx, r.ID)
	if got.State != model.ReminderFailed {
		t.Fatalf("state = %s", got.State)
	}
}

func TestReminderFailedGivingUpKeepsRecurringSeriesAlive(t *testing.T) {
	e := newEnv(t)
	e.svc.retry = func(int) (time.Duration, bool) { return 0, true }
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	r := e.claimOne(e.svc, *at(18, 0))
	e.must(e.svc.ReminderFailed(ctx, r, e.clk.Now(), false))
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	if want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC); !got.DueAt.Equal(want) {
		t.Fatalf("series stopped: due %v", got.DueAt)
	}
	e.clk.Set(time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC))
	if rs := e.claim(e.svc, e.clk.Now()); len(rs) != 1 {
		t.Fatalf("next occurrence reminders = %d", len(rs))
	}
}

func TestCancelReminder(t *testing.T) {
	e := newEnv(t)
	e.add(alice, "x", at(13, 0), nil)
	r := e.claimOne(e.svc, *at(13, 0))
	e.must(e.svc.CancelReminder(ctx, r))
	got, _ := e.st.GetReminder(ctx, r.ID)
	if got.State != model.ReminderCancelled {
		t.Fatalf("state = %s", got.State)
	}
}

// Two nodes on two handles of the same database file.

func TestTwoNodesClaimOnce(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	e.add(alice, "x", at(13, 0), nil)
	e.clk.Set(*at(13, 0))
	ra := e.claim(e.svc, e.clk.Now())
	rb := e.claim(b, e.clk.Now())
	if len(ra)+len(rb) != 1 {
		t.Fatalf("claimed by A=%d B=%d", len(ra), len(rb))
	}
}

func TestExpiredClaimCanBeClaimedAgain(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	e.add(alice, "x", at(13, 0), nil)
	e.clk.Set(*at(13, 0))
	if len(e.claim(e.svc, e.clk.Now())) != 1 {
		t.Fatal("A did not claim")
	}
	e.clk.Advance(defaultClaimTTL - time.Second)
	if len(e.claim(b, e.clk.Now())) != 0 {
		t.Fatal("B claimed before expiry")
	}
	e.clk.Advance(time.Second)
	if len(e.claim(b, e.clk.Now())) != 1 {
		t.Fatal("B could not reclaim after expiry")
	}
}

func TestNextWakeupCountsOtherNodesClaimFromItsExpiry(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	e.add(alice, "x", at(13, 0), nil)
	e.clk.Set(*at(13, 0))
	e.claim(e.svc, e.clk.Now())
	next, ok, err := b.NextWakeup(ctx)
	e.must(err)
	if want := at(13, 0).Add(defaultClaimTTL); !ok || !next.Equal(want) {
		t.Fatalf("next = %v %v, want %v", next, ok, want)
	}
}

func TestNextWakeupMinOfRemindersAndDigests(t *testing.T) {
	e := newEnv(t)
	e.add(alice, "x", at(20, 0), nil)
	e.must(e.svc.SetDigest(ctx, alice, "18:00"))
	next, ok, err := e.svc.NextWakeup(ctx)
	e.must(err)
	if !ok || !next.Equal(*at(18, 0)) {
		t.Fatalf("next = %v %v", next, ok)
	}
	e.must(e.svc.SetDigest(ctx, alice, "21:00"))
	next, _, _ = e.svc.NextWakeup(ctx)
	if !next.Equal(*at(20, 0)) {
		t.Fatalf("next = %v", next)
	}
}

func TestLateFailureAfterReclaimLeavesReminderSent(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	e.add(alice, "x", at(13, 0), nil)
	e.clk.Set(*at(13, 0))
	rA := e.claim(e.svc, e.clk.Now())[0].Reminder
	e.clk.Advance(defaultClaimTTL)
	rB := e.claim(b, e.clk.Now())
	if len(rB) != 1 {
		t.Fatal("B did not reclaim")
	}
	e.must(b.ReminderSent(ctx, rB[0].ID, 9, e.clk.Now()))
	e.must(e.svc.ReminderFailed(ctx, rA, e.clk.Now(), true))
	got, _ := e.st.GetReminder(ctx, rA.ID)
	if got.State != model.ReminderSent || got.MessageID != 9 {
		t.Fatalf("reminder = %+v", got)
	}
	e.must(e.svc.CancelReminder(ctx, rA))
	got, _ = e.st.GetReminder(ctx, rA.ID)
	if got.State != model.ReminderSent {
		t.Fatalf("cancel by stale claim changed it: %+v", got)
	}
}

func TestTwoNodesRecurringAdvancesOnce(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	e.clk.Set(*at(18, 0))
	r := e.claim(e.svc, e.clk.Now())[0]
	e.must(e.svc.ReminderSent(ctx, r.ID, 1, e.clk.Now()))
	e.must(b.ReminderSent(ctx, r.ID, 1, e.clk.Now()))
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	if want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC); !got.DueAt.Equal(want) {
		t.Fatalf("due = %v", got.DueAt)
	}
	e.clk.Set(time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC))
	if rs := e.claim(b, e.clk.Now()); len(rs) != 1 {
		t.Fatalf("pending after two ReminderSent = %d", len(rs))
	}
}

func TestDigestClaimSurvivesMigrateChat(t *testing.T) {
	e := newEnv(t)
	group := Actor{UserID: 7, ChatID: -100, ChatKind: model.ChatGroup, ChatTitle: "g"}
	e.must(e.svc.SetDigest(ctx, group, "09:00"))
	e.clk.Set(time.Date(2026, 10, 11, 9, 0, 5, 0, time.UTC))
	cs, err := e.svc.ClaimDigests(ctx, e.clk.Now())
	e.must(err)
	if len(cs) != 1 || cs[0].DigestClaimToken == "" {
		t.Fatalf("claimed = %+v", cs)
	}
	e.must(e.svc.MigrateChat(ctx, -100, -1001234))
	c := cs[0]
	c.ID = -1001234
	e.must(e.svc.DigestDone(ctx, c, e.clk.Now()))
	got, err := e.st.GetChat(ctx, -1001234)
	e.must(err)
	if want := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC); got.DigestNextAt == nil || !got.DigestNextAt.Equal(want) {
		t.Fatalf("digest next = %v, want %v", got.DigestNextAt, want)
	}
	// migrating again from the other node is harmless
	e.must(e.svc.MigrateChat(ctx, -100, -1001234))
}

func TestDigestDoneWithLostClaimIsNoop(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	e.must(e.svc.SetDigest(ctx, alice, "09:00"))
	e.clk.Set(time.Date(2026, 10, 11, 9, 0, 5, 0, time.UTC))
	ca, _ := e.svc.ClaimDigests(ctx, e.clk.Now())
	e.clk.Advance(defaultClaimTTL)
	cb, _ := b.ClaimDigests(ctx, e.clk.Now())
	if len(ca) != 1 || len(cb) != 1 {
		t.Fatalf("claims A=%d B=%d", len(ca), len(cb))
	}
	e.must(e.svc.DigestDone(ctx, ca[0], e.clk.Now())) // stale: dropped
	got, _ := e.st.GetChat(ctx, 7)
	if !got.DigestNextAt.Equal(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("stale DigestDone advanced: %v", got.DigestNextAt)
	}
	e.must(b.DigestDone(ctx, cb[0], e.clk.Now()))
	got, _ = e.st.GetChat(ctx, 7)
	if !got.DigestNextAt.Equal(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("digest next = %v", got.DigestNextAt)
	}
}

func TestPromptsUpdatesAndHousekeep(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	e.must(e.svc.PutPrompt(ctx, model.Prompt{ChatID: 7, MsgID: 5, UserID: 7, Kind: model.PromptEdit, TodoID: 1}))
	if _, err := b.TakePrompt(ctx, 7, 5, 8); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user's take = %v", err)
	}
	p, err := b.TakePrompt(ctx, 7, 5, 7)
	e.must(err)
	if !p.ExpiresAt.Equal(t0.Add(defaultPromptTTL)) || p.Kind != model.PromptEdit {
		t.Fatalf("prompt = %+v", p)
	}
	if _, err := e.svc.TakePrompt(ctx, 7, 5, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second take = %v", err)
	}
	e.must(e.svc.PutPrompt(ctx, model.Prompt{ChatID: 7, MsgID: 6, UserID: 7, Kind: model.PromptRemind, TodoID: 1}))
	e.clk.Advance(defaultPromptTTL + time.Second)
	if _, err := e.svc.TakePrompt(ctx, 7, 6, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired take = %v", err)
	}

	first, err := e.svc.MarkUpdate(ctx, 100)
	e.must(err)
	again, _ := b.MarkUpdate(ctx, 100)
	if !first || again {
		t.Fatalf("first=%v again=%v", first, again)
	}
	e.must(e.svc.Housekeep(ctx, e.clk.Now()))
}

func TestConfigPromptTTL(t *testing.T) {
	e := newEnv(t)
	s := New(e.st, e.clk, Config{PromptTTL: time.Minute})
	e.must(s.PutPrompt(ctx, model.Prompt{ChatID: 1, MsgID: 1, UserID: 1, Kind: model.PromptEdit}))
	e.clk.Advance(2 * time.Minute)
	if _, err := s.TakePrompt(ctx, 1, 1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("take after custom TTL = %v", err)
	}
}

// Integration with the real scheduler.

func newScheduler(e *env, s *Svc, snd *tgtest.FakeSender) *scheduler.Scheduler {
	return scheduler.New(s, snd, e.clk, quiet())
}

func TestSchedulerRetriesThenFails(t *testing.T) {
	e := newEnv(t)
	e.svc.retry = func(attempts int) (time.Duration, bool) { return time.Minute, attempts >= 3 }
	td := e.add(alice, "x", at(13, 0), nil)
	snd := &tgtest.FakeSender{ErrFn: func(int64, tg.OutMessage) error { return errors.New("boom") }}
	sc := newScheduler(e, e.svc, snd)
	e.clk.Set(*at(13, 0))
	for i := 0; i < 3; i++ {
		sc.Tick(ctx)
		e.clk.Advance(time.Minute)
	}
	var st model.ReminderState
	rs, _ := e.st.GetReminder(ctx, 1)
	st = rs.State
	if st != model.ReminderFailed || rs.Attempts != 3 {
		t.Fatalf("reminder = %+v", rs)
	}
	_ = td
}

func TestSchedulerSkipsDigestMoreThanAnHourLate(t *testing.T) {
	e := newEnv(t)
	e.add(alice, "overdue", dayPtr(2026, 10, 9, 10), nil)
	e.must(e.svc.SetDigest(ctx, alice, "09:00")) // next: 10-11 09:00
	snd := &tgtest.FakeSender{}
	sc := newScheduler(e, e.svc, snd)

	e.clk.Set(time.Date(2026, 10, 11, 10, 30, 0, 0, time.UTC)) // 1h30 late
	sc.Tick(ctx)
	if len(snd.Sent) != 0 {
		t.Fatalf("late digest sent: %d messages", len(snd.Sent))
	}
	c, _ := e.st.GetChat(ctx, 7)
	if want := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC); !c.DigestNextAt.Equal(want) {
		t.Fatalf("next digest = %v, want %v", c.DigestNextAt, want)
	}

	e.clk.Set(time.Date(2026, 10, 12, 9, 0, 30, 0, time.UTC)) // on time
	sc.Tick(ctx)
	if len(snd.Sent) != 1 {
		t.Fatalf("on-time digest: %d messages", len(snd.Sent))
	}
}

func TestSchedulerRecurringEndToEnd(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	snd := &tgtest.FakeSender{}
	sc := newScheduler(e, e.svc, snd)
	e.clk.Set(*at(18, 0))
	sc.Tick(ctx)
	sc.Tick(ctx) // nothing more is due
	if len(snd.Sent) != 1 {
		t.Fatalf("sent %d", len(snd.Sent))
	}
	got, _ := e.svc.Todo(ctx, alice, td.ID)
	if !got.DueAt.Equal(time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("due = %v", got.DueAt)
	}
}

func TestWakeOnMutations(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "x", at(18, 0), nil)
	steps := []struct {
		name string
		f    func()
	}{
		{"Remind", func() { e.mustRemind(td.ID, t0.Add(time.Hour)) }},
		{"SetDigest", func() { e.must(e.svc.SetDigest(ctx, alice, "09:00")) }},
		{"SetTZ", func() { e.must(e.svc.SetTZ(ctx, 7, "Asia/Tehran")) }},
		{"Complete", func() { _, err := e.svc.Complete(ctx, alice, td.ID); e.must(err) }},
		{"Delete", func() { e.must(e.svc.Delete(ctx, alice, td.ID)) }},
	}
	for _, st := range steps {
		name, f := st.name, st.f
		before := e.waker.n.Load()
		f()
		if e.waker.n.Load() == before {
			t.Errorf("%s did not Wake", name)
		}
	}
}
