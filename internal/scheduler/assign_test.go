package scheduler_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

const (
	assigneeID = 555
	groupChat  = 101 // addReminder(1): chat = 100+id
)

var (
	enLoc  = i18n.Locale{Lang: i18n.En}
	faLoc  = i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}
	tehran = time.FixedZone("IRST", 3*3600+1800)
)

// assignedEnv has one open assigned item (reminder 1) in group chat 101,
// created by user 7 (English, UTC) for user 555 (Farsi, Jalali, Tehran).
func assignedEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv()
	e.b.addReminder(1, t0, model.TodoOpen)
	e.b.rows[0].dr.Todo.AssigneeID = assigneeID
	e.b.rows[0].dr.Todo.AssigneeName = "wife"
	e.b.prefs[assigneeID] = model.Prefs{TZ: tehran, Locale: faLoc}
	return e
}

func forbidden(chat int64) func(int64, tg.OutMessage) error {
	return func(c int64, _ tg.OutMessage) error {
		if c == chat {
			return &tg.APIError{Code: 403, Desc: "Forbidden: bot can't initiate conversation with a user"}
		}
		return nil
	}
}

func TestAssignedReminderGoesToAssigneeInTheirPrefs(t *testing.T) {
	e := assignedEnv(t)
	e.s.Tick(context.Background())

	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != assigneeID {
		t.Fatalf("messages = %+v, want one DM to %d", msgs, assigneeID)
	}
	dr := e.b.rows[0].dr
	want := ui.ReminderMsg(dr, faLoc, tehran, t0, false)
	if msgs[0].Msg.Text != want.Text || len(msgs[0].Msg.Keyboard) != len(want.Keyboard) {
		t.Errorf("DM = %+v, want assignee-rendered %+v", msgs[0].Msg, want)
	}
	if len(want.Keyboard) == 0 || want.Keyboard[0][0].Text != faLoc.T("ui.btn.done") {
		t.Errorf("DM must carry the Done/snooze buttons in the assignee's language: %+v", want.Keyboard)
	}
	if got := msgs[0].Msg.Keyboard[0][0].Text; got != faLoc.T("ui.btn.done") {
		t.Errorf("Done button = %q, want assignee-language label", got)
	}
	if e.b.sentCount(1) != 1 || len(e.b.failed[1]) != 0 {
		t.Errorf("sent=%d failed=%v", e.b.sentCount(1), e.b.failed[1])
	}
}

func TestAssignedLateReminderUsesAssigneeLocale(t *testing.T) {
	e := assignedEnv(t)
	e.b.rows[0].dr.FireAt = t0.Add(-time.Hour)
	e.s.Tick(context.Background())
	msgs := e.snd.Messages()
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v", msgs)
	}
	want := ui.ReminderMsg(e.b.rows[0].dr, faLoc, tehran, t0, true).Text
	if msgs[0].Msg.Text != want {
		t.Errorf("text = %q, want %q", msgs[0].Msg.Text, want)
	}
}

func TestAssigneeIsRoutedForEveryKind(t *testing.T) {
	for _, k := range []model.ReminderKind{model.ReminderDue, model.ReminderCustom, model.ReminderSnooze} {
		e := assignedEnv(t)
		e.b.rows[0].dr.Kind = k
		e.s.Tick(context.Background())
		if m := e.snd.Messages(); len(m) != 1 || m[0].ChatID != assigneeID {
			t.Errorf("kind %v: messages = %+v", k, m)
		}
	}
}

func TestForbiddenDMFallsBackOnceToCreatingChat(t *testing.T) {
	e := assignedEnv(t)
	e.snd.ErrFn = forbidden(assigneeID)
	e.s.Tick(context.Background())

	msgs := e.snd.Messages() // FakeSender records only successful sends
	if len(msgs) != 1 || msgs[0].ChatID != groupChat {
		t.Fatalf("messages = %+v, want exactly one fallback to %d", msgs, groupChat)
	}
	text := msgs[0].Msg.Text
	if !strings.HasPrefix(text, ui.Mention(assigneeID, "wife", enLoc)) {
		t.Errorf("fallback must start with the mention, got %q", text)
	}
	if !strings.Contains(text, "todo 1") {
		t.Errorf("fallback lost the item text: %q", text)
	}
	if e.b.sentCount(1) != 1 || len(e.b.failed[1]) != 0 {
		t.Errorf("sent=%d failed=%v, want sent once", e.b.sentCount(1), e.b.failed[1])
	}
	if got := msgs[0].Msg.Keyboard[0][0].Text; got != enLoc.T("ui.btn.done") {
		t.Errorf("fallback buttons must use the creator's language, got %q", got)
	}
	// The reminder is done: a later tick must not try again.
	e.snd.Reset()
	e.s.Tick(context.Background())
	if len(e.snd.Messages()) != 0 {
		t.Error("reminder re-sent after fallback")
	}
}

func TestFallbackAttemptsDMExactlyOncePerClaim(t *testing.T) {
	e := assignedEnv(t)
	calls := map[int64]int{}
	e.snd.ErrFn = func(c int64, m tg.OutMessage) error {
		calls[c]++
		return forbidden(assigneeID)(c, m)
	}
	e.s.Tick(context.Background())
	if calls[assigneeID] != 1 || calls[groupChat] != 1 {
		t.Errorf("send attempts = %v, want one DM and one fallback", calls)
	}
}

func TestTransientDMErrorBacksOffWithoutFallback(t *testing.T) {
	e := assignedEnv(t)
	e.snd.ErrFn = func(c int64, _ tg.OutMessage) error {
		if c == assigneeID {
			return &tg.APIError{Code: 502, Desc: "Bad Gateway"}
		}
		return nil
	}
	e.s.Tick(context.Background())
	if len(e.snd.Messages()) != 0 {
		t.Errorf("transient DM error must not fall back: %+v", e.snd.Messages())
	}
	if f := e.b.failed[1]; len(f) != 1 || f[0] {
		t.Errorf("failed = %v, want one transient failure", f)
	}
	if e.b.sentCount(1) != 0 {
		t.Error("must not be marked sent")
	}
}

func TestFallbackFailureIsRecordedWithItsPermanence(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"transient", &tg.APIError{Code: 502, Desc: "Bad Gateway"}, false},
		{"permanent", &tg.APIError{Code: 403, Desc: "Forbidden: bot was kicked"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := assignedEnv(t)
			e.snd.ErrFn = func(c int64, _ tg.OutMessage) error {
				if c == assigneeID {
					return &tg.APIError{Code: 403, Desc: "Forbidden"}
				}
				return tc.err
			}
			e.s.Tick(context.Background())
			if f := e.b.failed[1]; len(f) != 1 || f[0] != tc.want {
				t.Errorf("failed = %v, want [%v]", f, tc.want)
			}
			if e.b.sentCount(1) != 0 {
				t.Error("must not be marked sent")
			}
		})
	}
}

func TestAssigneePrefsErrorFailsTransientlyWithoutSending(t *testing.T) {
	e := assignedEnv(t)
	e.b.prefsErrFor[assigneeID] = errBoom // the creator's prefs succeed
	e.s.Tick(context.Background())
	if f := e.b.failed[1]; len(f) != 1 || f[0] {
		t.Errorf("failed = %v", f)
	}
	if len(e.snd.Messages()) != 0 {
		t.Error("nothing should be sent")
	}
	if !slices.Contains(e.b.prefsCalls, int64(assigneeID)) {
		t.Error("assignee prefs were never requested")
	}
}

func TestAssigneeEqualToCreatingChatSendsOnce(t *testing.T) {
	e := assignedEnv(t)
	e.b.rows[0].dr.Todo.AssigneeID = groupChat // defensive: the service stores no such assignee
	e.b.prefs[groupChat] = model.Prefs{TZ: tehran, Locale: faLoc}
	e.s.Tick(context.Background())
	m := e.snd.Messages()
	if len(m) != 1 || m[0].ChatID != groupChat {
		t.Fatalf("messages = %+v", m)
	}
	want := ui.ReminderMsg(e.b.rows[0].dr, enLoc, time.UTC, t0, false)
	if m[0].Msg.Text != want.Text || m[0].Msg.Keyboard[0][0].Text != enLoc.T("ui.btn.done") {
		t.Errorf("must render with the creator's prefs: %+v", m[0].Msg)
	}
}

func TestDMAndFallbackRecordTheirMessageIDs(t *testing.T) {
	e := assignedEnv(t)
	e.s.Tick(context.Background())
	dm := e.snd.Messages()
	if len(dm) != 1 || e.b.sentMsgID[1] != dm[0].MsgID {
		t.Errorf("DM: recorded %d, sent %+v", e.b.sentMsgID[1], dm)
	}

	e = assignedEnv(t)
	e.snd.ErrFn = forbidden(assigneeID)
	e.s.Tick(context.Background())
	fb := e.snd.Messages()
	if len(fb) != 1 || e.b.sentMsgID[1] != fb[0].MsgID || fb[0].MsgID == 0 {
		t.Errorf("fallback: recorded %d, sent %+v", e.b.sentMsgID[1], fb)
	}
}

func TestDMRateLimitWithinBudgetThenForbiddenFallsBackOnce(t *testing.T) {
	e := assignedEnv(t)
	dmCalls := 0
	e.snd.ErrFn = func(c int64, m tg.OutMessage) error {
		if c != assigneeID {
			return nil
		}
		dmCalls++
		if dmCalls == 1 {
			return &tg.APIError{Code: 429, Desc: "Too Many Requests", RetryAfter: 10 * time.Second}
		}
		return forbidden(assigneeID)(c, m)
	}
	done := make(chan struct{})
	go func() { defer close(done); e.s.Tick(context.Background()) }()
	eventually(t, "tick to wait on the clock", func() bool { return e.clk.Waiters() == 1 })
	e.clk.Advance(10 * time.Second)
	<-done

	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != groupChat || dmCalls != 2 {
		t.Fatalf("messages = %+v, dm attempts = %d", msgs, dmCalls)
	}
	if e.b.sentCount(1) != 1 || len(e.b.failed[1]) != 0 {
		t.Errorf("sent=%d failed=%v", e.b.sentCount(1), e.b.failed[1])
	}
}

func TestDMRateLimitBeyondBudgetFailsTransientlyWithoutFallback(t *testing.T) {
	e := assignedEnv(t)
	e.snd.ErrFn = func(c int64, _ tg.OutMessage) error {
		if c == assigneeID {
			return &tg.APIError{Code: 429, Desc: "Too Many Requests", RetryAfter: 60 * time.Second} // == ttl/2
		}
		return nil
	}
	e.s.Tick(context.Background())
	if len(e.snd.Messages()) != 0 {
		t.Errorf("no fallback on a 429: %+v", e.snd.Messages())
	}
	if f := e.b.failed[1]; len(f) != 1 || f[0] {
		t.Errorf("failed = %v, want one transient failure", f)
	}
}

func TestFallbackChatMigrationLandsInNewChatOnce(t *testing.T) {
	e := assignedEnv(t)
	calls := map[int64]int{}
	e.snd.ErrFn = func(c int64, m tg.OutMessage) error {
		calls[c]++
		switch c {
		case assigneeID:
			return forbidden(assigneeID)(c, m)
		case groupChat:
			return &tg.APIError{Code: 400, Desc: "group chat was upgraded to a supergroup chat", MigrateTo: -1009}
		}
		return nil
	}
	e.s.Tick(context.Background())
	msgs := e.snd.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != -1009 {
		t.Fatalf("messages = %+v", msgs)
	}
	if calls[assigneeID] != 1 || len(e.b.migrated) != 1 || e.b.migrated[0] != [2]int64{groupChat, -1009} {
		t.Errorf("dm attempts=%d migrated=%v", calls[assigneeID], e.b.migrated)
	}
	if e.b.sentCount(1) != 1 {
		t.Error("should be marked sent")
	}
}

func TestUnassignedStillGoesToCreatingChatInCreatorPrefs(t *testing.T) {
	e := assignedEnv(t)
	e.b.rows[0].dr.Todo.AssigneeID = 0
	e.s.Tick(context.Background())
	if m := e.snd.Messages(); len(m) != 1 || m[0].ChatID != groupChat {
		t.Fatalf("messages = %+v", m)
	}
	for _, id := range e.b.prefsCalls {
		if id == assigneeID {
			t.Error("assignee prefs must not be read for an unassigned item")
		}
	}
}
