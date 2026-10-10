package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
)

// Scenario 4a: a one-off reminder that came due while the bot was down is
// still sent, and says it is late.
func TestCatchUpOneOffIsSentLate(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/add pay rent in 1h")
	// The bot is "down" from 12:00 to 15:00: the clock moves, nothing ticks.
	clk.Set(utc(2026, 10, 10, 15, 0))
	if n := len(reminders(s.send, alice)); n != 0 {
		t.Fatalf("%d reminders sent while down", n)
	}
	s.tick() // startup tick

	got := reminders(s.send, alice)
	if len(got) != 1 {
		t.Fatalf("got %d reminders after the downtime, want 1", len(got))
	}
	text := got[0].Msg.Text
	if !strings.Contains(text, "pay rent") || !strings.Contains(text, "late — was due") {
		t.Errorf("reminder text %q lacks the late marker", text)
	}
	if !strings.Contains(text, "13:00") {
		t.Errorf("late marker %q does not say it was due at 13:00", text)
	}

	// A reminder less than two minutes late carries no marker.
	s.say(alice, alice, "/add stretch in 1m")
	s.advance(2*time.Minute - time.Second + time.Minute) // 2m59s: due 1m, so 1m59s late
	got = reminders(s.send, alice)
	if len(got) != 2 || strings.Contains(got[1].Msg.Text, "late") {
		t.Errorf("a reminder under 2m late was marked late: %+v", got)
	}
}

// Scenario 4b: several occurrences of a recurring reminder that were missed
// collapse into one late message, and the next one is in the future.
func TestCatchUpRecurringCollapses(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/add water plants every day at 9:00")
	if td := s.todoByNum(alice, 1); td.DueAt == nil || !td.DueAt.Equal(utc(2026, 10, 11, 9, 0)) {
		t.Fatalf("first due %v", td.DueAt)
	}
	// Down for three and a half days: the 11th, 12th, 13th and 14th at 09:00
	// were all missed.
	clk.Set(utc(2026, 10, 14, 21, 0))
	s.tick()

	got := reminders(s.send, alice)
	if len(got) != 1 {
		t.Fatalf("got %d reminders for 4 missed occurrences, want 1", len(got))
	}
	if !strings.Contains(got[0].Msg.Text, "late — was due") {
		t.Errorf("collapsed reminder %q lacks the late marker", got[0].Msg.Text)
	}
	if td := s.todoByNum(alice, 1); td.DueAt == nil || !td.DueAt.Equal(utc(2026, 10, 15, 9, 0)) {
		t.Errorf("next due %v, want 2026-10-15 09:00 UTC", td.DueAt)
	}
	// Nothing else is pending for the missed days.
	s.tick()
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Errorf("%d reminders after a second tick, want 1", n)
	}
	// ... and the series carries on at 09:00.
	s.advanceTo(utc(2026, 10, 15, 9, 0))
	got = reminders(s.send, alice)
	if len(got) != 2 || strings.Contains(got[1].Msg.Text, "late") {
		t.Errorf("series did not resume on time: %+v", got)
	}
}

// Scenario 4c: a digest that is up to an hour late is sent; one that is more
// than an hour late is skipped and moves to the next day.
func TestCatchUpDigest(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/add buy milk") // no date, so the digest has content
	s.say(alice, alice, "/digest 08:00")
	assertDigestNext(t, s, alice, utc(2026, 10, 11, 8, 0))

	// Down until 09:30 on the 11th: 1h30 late, so the digest is skipped.
	s.advanceTo(utc(2026, 10, 11, 9, 30))
	if n := countDigests(s, alice); n != 0 {
		t.Fatalf("a digest 1h30 late was sent (%d)", n)
	}
	assertDigestNext(t, s, alice, utc(2026, 10, 12, 8, 0))

	// Down until 08:45 on the 12th: 45m late, so it is still sent.
	s.advanceTo(utc(2026, 10, 12, 8, 45))
	if n := countDigests(s, alice); n != 1 {
		t.Fatalf("a digest 45m late was not sent (%d)", n)
	}
	assertDigestNext(t, s, alice, utc(2026, 10, 13, 8, 0))

	// The limit is "more than 1h late" (scheduler: now-next > digestMaxLate),
	// so a digest exactly 1h late is still sent ...
	s.advanceTo(utc(2026, 10, 13, 9, 0))
	if n := countDigests(s, alice); n != 2 {
		t.Fatalf("a digest exactly 1h late was not sent (%d digests)", n)
	}
	assertDigestNext(t, s, alice, utc(2026, 10, 14, 8, 0))

	// ... and one second more is skipped.
	s.advanceTo(utc(2026, 10, 14, 9, 0).Add(time.Second))
	if n := countDigests(s, alice); n != 2 {
		t.Fatalf("a digest 1h and 1s late was sent (%d digests)", n)
	}
	assertDigestNext(t, s, alice, utc(2026, 10, 15, 8, 0))
}

func countDigests(s *stack, chat int64) int {
	n := 0
	for _, m := range s.send.Messages() {
		if !m.Edit && m.ChatID == chat && strings.Contains(m.Msg.Text, "without date") {
			n++
		}
	}
	return n
}

func assertDigestNext(t *testing.T, s *stack, chat int64, want time.Time) {
	t.Helper()
	c, err := s.st.GetChat(t.Context(), chat)
	if err != nil {
		t.Fatal(err)
	}
	if c.DigestNextAt == nil || !c.DigestNextAt.Equal(want) {
		t.Errorf("digest next at %v, want %v", c.DigestNextAt, want)
	}
}
