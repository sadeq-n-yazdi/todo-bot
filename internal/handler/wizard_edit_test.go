package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Edit wizard tests (#98, #106). They run the handler on the real service
// and a SQLite database. The clock is Saturday 2026-10-10 12:00 UTC and the
// default time is 09:00.

func utcT(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func actorFor(chat, user int64) service.Actor {
	kind := model.ChatPrivate
	if chat < 0 {
		kind = model.ChatGroup
	}
	return service.Actor{UserID: user, ChatID: chat, ChatKind: kind}
}

// add stores an open item by wzAlice in chat. due nil = no date; rule ""
// = once; assignee 0 = nobody.
func (e *wzEnv) add(chat int64, text string, due *time.Time, rule string, assignee int64) model.Todo {
	e.t.Helper()
	res := nlparse.Result{Text: text, Due: due, HasTime: due != nil}
	if rule != "" {
		rl, err := recur.Parse(rule)
		if err != nil {
			e.t.Fatal(err)
		}
		res.Rule = &rl
	}
	td, err := e.svc.AddTodoFor(context.Background(), actorFor(chat, wzAlice), assignee, res)
	if err != nil {
		e.t.Fatal(err)
	}
	return td
}

func (e *wzEnv) todoByID(chat, id int64) model.Todo {
	e.t.Helper()
	td, err := e.svc.Todo(context.Background(), actorFor(chat, wzAlice), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return td
}

// pressEditBtn presses the list's ✏️ button of td as user.
func (e *wzEnv) pressEditBtn(user, chat int64, td model.Todo) string {
	e.t.Helper()
	return e.press(user, chat, 5, ui.Encode(ui.Action{Kind: ui.ActEdit, ID: td.ID}))
}

func (e *wzEnv) lc(user int64) i18n.Locale {
	e.t.Helper()
	p, err := e.svc.Prefs(context.Background(), user, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return p.Locale
}

// sendsTo returns the new (not edited) messages sent to chat.
func (e *wzEnv) sendsTo(chat int64) []string {
	var out []string
	for _, m := range e.fs.Messages() {
		if !m.Edit && m.ChatID == chat {
			out = append(out, m.Msg.Text)
		}
	}
	return out
}

func TestEditPrefillRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		due    time.Time
		rule   string
		repeat string
		time   string // wanted DraftData.Time
	}{
		{"once", utcT(2026, 10, 14, 15, 0), "", model.RepeatOnce, "15:00"},
		{"once default time", utcT(2026, 10, 14, 9, 0), "", model.RepeatOnce, ""},
		{"daily", utcT(2026, 10, 14, 15, 0), "daily 15:00", model.RepeatDaily, "15:00"},
		{"weekly", utcT(2026, 10, 14, 15, 0), "weekly wed 15:00", model.RepeatWeekly, "15:00"},
		{"monthly", utcT(2026, 10, 14, 15, 0), "monthly 14 15:00", model.RepeatMonthly, "15:00"},
		{"yearly", utcT(2026, 10, 14, 15, 0), "yearly 10-14 15:00", model.RepeatYearly, "15:00"},
		{"nth weekday", utcT(2026, 10, 14, 15, 0), "monthly wed#2 15:00", model.RepeatNthWeekday, "15:00"},
		{"last weekday", utcT(2026, 10, 28, 15, 0), "monthly wed#last 15:00", model.RepeatLastWeekday, "15:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newWz(t)
			td := e.add(wzAlice, "x", &tc.due, tc.rule, 0)
			e.pressEditBtn(wzAlice, wzAlice, td)
			d := e.draft(wzAlice, wzAlice)
			dd := d.Data
			if d.Step != model.StepConfirm || dd.EditTodoID != td.ID || dd.EditNum != td.Num {
				t.Fatalf("draft %+v", d)
			}
			if dd.Date != tc.due.Format("2006-01-02") || dd.Time != tc.time || dd.Repeat != tc.repeat || dd.FoundDue != nil {
				t.Fatalf("prefill %+v", dd)
			}
			if dd.Text != "x" || dd.OrigText != "x" {
				t.Fatalf("text %+v", dd)
			}
			b := dd.EditBase
			if b == nil || b.EditBase != nil || b.EditTodoID != 0 || b.EditNum != 0 ||
				b.Date != dd.Date || b.Time != dd.Time || b.Repeat != dd.Repeat || b.Text != dd.Text {
				t.Fatalf("base %+v", b)
			}
			// Saving at once changes nothing.
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
			e.noDraft(wzAlice, wzAlice)
			got := e.todoByID(wzAlice, td.ID)
			if got.Recur != td.Recur || !got.DueAt.Equal(*td.DueAt) || !got.UpdatedAt.Equal(td.UpdatedAt) {
				t.Fatalf("item changed: %+v -> %+v", td, got)
			}
		})
	}
}

func TestEditPrefillNoDate(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "x", nil, "", 0)
	e.pressEditBtn(wzAlice, wzAlice, td)
	dd := e.draft(wzAlice, wzAlice).Data
	if dd.Date != "" || dd.Time != "" || dd.Repeat != "" || dd.FoundDue != nil {
		t.Fatalf("prefill %+v", dd)
	}
}

func TestEditPrefillNonRoundTripGoesToFound(t *testing.T) {
	tests := []struct {
		name string
		due  time.Time
		rule string
	}{
		{"every N days", utcT(2026, 10, 14, 15, 0), "every 3d 2026-10-14 15:00"},
		{"two weekdays", utcT(2026, 10, 14, 15, 0), "weekly mon,wed 15:00"},
		{"clamped month day", utcT(2026, 11, 30, 15, 0), "monthly 31 15:00"},
		{"jalali rule for a Gregorian user", utcT(2026, 10, 14, 15, 0), "monthly 22 15:00 jalali"},
		{"leap day in a common year", utcT(2027, 2, 28, 15, 0), "yearly 02-29 15:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newWz(t)
			td := e.add(wzAlice, "x", &tc.due, tc.rule, 0)
			e.pressEditBtn(wzAlice, wzAlice, td)
			dd := e.draft(wzAlice, wzAlice).Data
			if dd.Date != "" || dd.Time != "" || dd.Repeat != "" || dd.FoundDue == nil ||
				!dd.FoundDue.Equal(*td.DueAt) || dd.FoundRule != td.Recur || !dd.FoundTime {
				t.Fatalf("prefill %+v (item %+v)", dd, td)
			}
			if dd.EditBase == nil || dd.EditBase.FoundRule != td.Recur {
				t.Fatalf("base %+v", dd.EditBase)
			}
			// A text-only edit keeps the rule exactly.
			d := e.draft(wzAlice, wzAlice)
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavEdit)
			e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditText)
			e.msg(wzAlice, wzAlice, "renamed", 0)
			e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
			got := e.todoByID(wzAlice, td.ID)
			if got.Text != "renamed" || got.Recur != td.Recur || !got.DueAt.Equal(*td.DueAt) {
				t.Fatalf("item %+v", got)
			}
		})
	}
}

func TestEditCardIsAnEditCard(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "milk", nil, "", 0)
	e.fs.Reset()
	e.pressEditBtn(wzAlice, wzAlice, td)
	lc := e.lc(wzAlice)
	card := e.lastEdit()
	if !strings.Contains(card.Msg.Text, lc.T("ui.wizard.edit.head", lc.Num(td.Num))) {
		t.Fatalf("not an edit card: %q", card.Msg.Text)
	}
	if first := e.fs.Messages()[0]; first.Edit || first.Msg.Keyboard[0][0].Data != "wz:0" {
		t.Fatalf("a new placeholder card comes first: %+v", first)
	}
}

func TestEditNoChange(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "milk", nil, "", 0)
	e.pressEditBtn(wzAlice, wzAlice, td)
	e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
	e.noDraft(wzAlice, wzAlice)
	if got := e.lastEdit().Msg.Text; got != e.lc(wzAlice).T("wizard.edit.nochange") {
		t.Fatalf("card %q", got)
	}
}

func TestEditCancel(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "milk", nil, "", 0)
	e.pressEditBtn(wzAlice, wzAlice, td)
	e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavCancel)
	e.noDraft(wzAlice, wzAlice)
	if got := e.lastEdit().Msg.Text; got != e.lc(wzAlice).T("wizard.edit.cancelled") {
		t.Fatalf("card %q", got)
	}
}

// editText changes the text of the open edit wizard of wzAlice in chat.
func (e *wzEnv) editText(chat int64, text string) {
	e.t.Helper()
	d := e.draft(chat, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavEdit)
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditText)
	d = e.draft(chat, wzAlice)
	var replyTo int
	if chat < 0 {
		replyTo = d.AskMsgID
	}
	e.msg(wzAlice, chat, text, replyTo)
}

func TestEditOverdueItemTextOnlySaves(t *testing.T) {
	e := newWz(t)
	due := utcT(2026, 10, 11, 10, 0)
	td := e.add(wzAlice, "old", &due, "", 0)
	e.clk.Advance(48 * time.Hour) // the item is overdue now
	e.pressEditBtn(wzAlice, wzAlice, td)
	e.editText(wzAlice, "new")
	if d := e.draft(wzAlice, wzAlice); d.Step != model.StepConfirm || d.Data.Text != "new" {
		t.Fatalf("draft %+v", d)
	}
	e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
	e.noDraft(wzAlice, wzAlice)
	got := e.todoByID(wzAlice, td.ID)
	if got.Text != "new" || !got.DueAt.Equal(due) || got.Num != td.Num {
		t.Fatalf("item %+v", got)
	}
	if last := e.lastEdit().Msg.Text; !strings.Contains(last, "new") || !strings.Contains(last, e.lc(wzAlice).Num(td.Num)) {
		t.Fatalf("card %q", last)
	}
}

func TestEditChangedScheduleInThePastIsRefused(t *testing.T) {
	e := newWz(t)
	due := utcT(2026, 10, 14, 10, 0)
	td := e.add(wzAlice, "x", &due, "", 0)
	e.pressEditBtn(wzAlice, wzAlice, td)
	d := e.draft(wzAlice, wzAlice)
	d.Data.Time = "08:00"
	d.Data.Date = "2026-10-10" // today, 08:00: has passed
	if err := e.svc.UpdateDraft(context.Background(), &d); err != nil {
		t.Fatal(err)
	}
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	if got := e.draft(wzAlice, wzAlice); got.Step != model.StepTime {
		t.Fatalf("the draft goes back to the time step: %+v", got)
	}
	if got := e.todoByID(wzAlice, td.ID); !got.DueAt.Equal(due) {
		t.Fatalf("item changed %+v", got)
	}
}

func TestEditGoneItemEndsDraft(t *testing.T) {
	for _, how := range []string{"done", "deleted"} {
		t.Run(how, func(t *testing.T) {
			e := newWz(t)
			td := e.add(wzAlice, "x", nil, "", 0)
			e.pressEditBtn(wzAlice, wzAlice, td)
			e.editText(wzAlice, "y")
			var err error
			if how == "done" {
				_, err = e.svc.Complete(context.Background(), actorFor(wzAlice, wzAlice), td.ID)
			} else {
				err = e.svc.Delete(context.Background(), actorFor(wzAlice, wzAlice), td.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
			e.noDraft(wzAlice, wzAlice)
			if got := e.lastEdit().Msg.Text; got != e.lc(wzAlice).T("wizard.edit.gone") {
				t.Fatalf("card %q", got)
			}
		})
	}
}

func TestEditForeignChatRefused(t *testing.T) {
	e := newWz(t)
	td := e.add(wzBob, "secret", nil, "", 0) // an item of Bob's private chat
	e.fs.Reset()
	toast := e.pressEditBtn(wzAlice, wzAlice, td)
	if toast != e.lc(wzAlice).T("settings.cb.gone") {
		t.Fatalf("toast %q", toast)
	}
	e.noDraft(wzAlice, wzAlice)
	if n := len(e.fs.Messages()); n != 0 {
		t.Fatalf("nothing may be sent: %+v", e.fs.Messages())
	}
}

func TestEditDoneItemRefused(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "x", nil, "", 0)
	if _, err := e.svc.Complete(context.Background(), actorFor(wzAlice, wzAlice), td.ID); err != nil {
		t.Fatal(err)
	}
	if toast := e.pressEditBtn(wzAlice, wzAlice, td); toast != e.lc(wzAlice).T("settings.cb.gone") {
		t.Fatalf("toast %q", toast)
	}
	e.noDraft(wzAlice, wzAlice)
}

func TestEditGroupPressByAnotherMemberGetsToast(t *testing.T) {
	e := newWz(t)
	td := e.add(wzGrp, "x", nil, "", 0)
	e.pressEditBtn(wzAlice, wzGrp, td)
	d := e.draft(wzGrp, wzAlice)
	toast := e.pressD(d, wzBob, ui.ActWizNav, ui.WizNavSave)
	if toast != e.lc(wzBob).T("wizard.toast.foreign") {
		t.Fatalf("toast %q", toast)
	}
	if got := e.draft(wzGrp, wzAlice); got.Version != d.Version {
		t.Fatal("the draft must not change")
	}
	// Bob can start his own edit of the same item.
	e.pressEditBtn(wzBob, wzGrp, td)
	if bd, err := e.svc.Draft(context.Background(), wzGrp, wzBob); err != nil || bd.Data.EditTodoID != td.ID {
		t.Fatalf("bob's draft %+v, %v", bd, err)
	}
}

func TestEditKeepsItemNumber(t *testing.T) {
	e := newWz(t)
	e.add(wzGrp, "a", nil, "", 0)
	b := e.add(wzGrp, "b", nil, "", 0)
	e.add(wzGrp, "c", nil, "", 0)
	e.pressEditBtn(wzAlice, wzGrp, b)
	e.editText(wzGrp, "b2")
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
	got := e.todoByID(wzGrp, b.ID)
	if got.Num != b.Num || got.Text != "b2" {
		t.Fatalf("item %+v", got)
	}
	byNum, err := e.svc.ByNum(context.Background(), wzGrp, b.Num)
	if err != nil || byNum.ID != b.ID {
		t.Fatalf("by num %+v, %v", byNum, err)
	}
}

func TestEditDateWalksToTimeAndRepeatThenConfirm(t *testing.T) {
	e := newWz(t)
	due := utcT(2026, 10, 14, 15, 0)
	td := e.add(wzAlice, "x", &due, "", 0)
	e.pressEditBtn(wzAlice, wzAlice, td)
	step := func(want model.DraftStep) model.Draft {
		t.Helper()
		d := e.draft(wzAlice, wzAlice)
		if d.Step != want {
			t.Fatalf("step %s, want %s", d.Step, want)
		}
		return d
	}
	d := step(model.StepConfirm)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavEdit)
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditDate)
	d = step(model.StepDate)
	e.pressD(d, wzAlice, ui.ActWizDate, "20261020")
	d = step(model.StepTime)
	if d.Data.Editing {
		t.Fatal("a new date walks on")
	}
	e.pressD(d, wzAlice, ui.ActWizTime, "1030")
	d = step(model.StepRepeat)
	e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatDaily)
	d = step(model.StepConfirm) // no assignee step in an edit
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	got := e.todoByID(wzAlice, td.ID)
	if !got.DueAt.Equal(utcT(2026, 10, 20, 10, 30)) || got.Recur != "daily 10:30" {
		t.Fatalf("item %+v", got)
	}
}

func TestEditBackFromDateGoesToConfirm(t *testing.T) {
	e := newWz(t)
	due := utcT(2026, 10, 14, 15, 0)
	td := e.add(wzAlice, "x", &due, "", 0)
	e.pressEditBtn(wzAlice, wzAlice, td)
	d := e.draft(wzAlice, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavEdit)
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditDate)
	d = e.draft(wzAlice, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizDate, "20261020")
	d = e.draft(wzAlice, wzAlice) // time step, Editing cleared
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavBack)
	d = e.draft(wzAlice, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavBack)
	if d = e.draft(wzAlice, wzAlice); d.Step != model.StepConfirm {
		t.Fatalf("back from the date step: %s", d.Step)
	}
}

// ---- the DM to a new assignee ----

// assignInGroup opens the edit wizard of td in the group and sets the
// assignee with the wizard buttons (arg is a WizAssign* value or an ID).
func (e *wzEnv) assignInGroup(td model.Todo, arg string) {
	e.t.Helper()
	e.pressEditBtn(wzAlice, wzGrp, td)
	d := e.draft(wzGrp, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavEdit)
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditAssignee)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, arg)
	if d = e.draft(wzGrp, wzAlice); d.Step != model.StepConfirm {
		e.t.Fatalf("step %s", d.Step)
	}
}

func (e *wzEnv) save(chat int64) {
	e.t.Helper()
	e.pressD(e.draft(chat, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
}

func TestEditDMToNewAssigneeInTheirLanguage(t *testing.T) {
	e := newWz(t)
	e.setLang(wzBob, i18n.Fa)
	due := utcT(2026, 10, 14, 15, 0)
	td := e.add(wzGrp, "buy milk", &due, "", 0)
	e.assignInGroup(td, "2")
	e.fs.Reset()
	e.save(wzGrp)

	if got := e.todoByID(wzGrp, td.ID); got.AssigneeID != wzBob || got.Num != td.Num {
		t.Fatalf("item %+v", got)
	}
	dms := e.sendsTo(wzBob)
	if len(dms) != 1 {
		t.Fatalf("DMs: %q", dms)
	}
	fa := e.lc(wzBob)
	if !strings.Contains(dms[0], "buy milk") || !strings.Contains(dms[0], "محول") ||
		strings.Contains(dms[0], "assigned you") {
		t.Fatalf("DM %q", dms[0])
	}
	if !strings.Contains(dms[0], "alice_u") || !strings.Contains(dms[0], fa.Digits("15:00")) {
		t.Fatalf("DM has the editor and the time in the assignee's locale: %q", dms[0])
	}
	for _, m := range e.fs.Messages() {
		if !m.Edit && m.ChatID == wzBob && len(m.Msg.Keyboard) != 0 {
			t.Fatal("the DM has no buttons")
		}
	}
	card := e.lastEdit().Msg.Text
	if strings.Contains(card, "did not get a message") {
		t.Fatalf("no note expected: %q", card)
	}
	// Saving again does nothing: the draft is gone, so no second DM.
	e.fs.Reset()
	e.pressD(model.Draft{ChatID: wzGrp, MsgID: 1, Version: 1}, wzAlice, ui.ActWizNav, ui.WizNavSave)
	if len(e.sendsTo(wzBob)) != 0 {
		t.Fatal("a second DM")
	}
}

func TestEditNoDMWhenAssigneeNotNew(t *testing.T) {
	t.Run("unchanged assignee", func(t *testing.T) {
		e := newWz(t)
		td := e.add(wzGrp, "x", nil, "", wzBob)
		e.pressEditBtn(wzAlice, wzGrp, td)
		e.editText(wzGrp, "y")
		e.fs.Reset()
		e.save(wzGrp)
		if got := e.todoByID(wzGrp, td.ID); got.Text != "y" || got.AssigneeID != wzBob {
			t.Fatalf("item %+v", got)
		}
		if dms := e.sendsTo(wzBob); len(dms) != 0 {
			t.Fatalf("DMs %q", dms)
		}
	})
	t.Run("cleared assignee", func(t *testing.T) {
		e := newWz(t)
		td := e.add(wzGrp, "x", nil, "", wzBob)
		e.assignInGroup(td, ui.WizAssignNone)
		e.fs.Reset()
		e.save(wzGrp)
		if got := e.todoByID(wzGrp, td.ID); got.AssigneeID != 0 {
			t.Fatalf("item %+v", got)
		}
		if dms := e.sendsTo(wzBob); len(dms) != 0 {
			t.Fatalf("DMs %q", dms)
		}
	})
	t.Run("self assignment", func(t *testing.T) {
		e := newWz(t)
		td := e.add(wzGrp, "x", nil, "", wzBob)
		e.assignInGroup(td, ui.WizAssignMe)
		e.fs.Reset()
		e.save(wzGrp)
		if got := e.todoByID(wzGrp, td.ID); got.AssigneeID != wzAlice {
			t.Fatalf("item %+v", got)
		}
		for _, id := range []int64{wzAlice, wzBob} {
			if dms := e.sendsTo(id); len(dms) != 0 {
				t.Fatalf("DMs to %d: %q", id, dms)
			}
		}
	})
}

func TestEditDMBlockedAddsNoteAndSaveSucceeds(t *testing.T) {
	e := newWz(t)
	td := e.add(wzGrp, "x", nil, "", 0)
	e.assignInGroup(td, "2")
	e.fs.ErrFn = func(chat int64, _ tg.OutMessage) error {
		if chat == wzBob {
			return &tg.APIError{Code: 403, Desc: "Forbidden: bot can't initiate conversation with a user"}
		}
		return nil
	}
	e.save(wzGrp)
	if got := e.todoByID(wzGrp, td.ID); got.AssigneeID != wzBob {
		t.Fatalf("Save must succeed: %+v", got)
	}
	e.noDraft(wzGrp, wzAlice)
	if card := e.lastEdit().Msg.Text; !strings.Contains(card, "@bob_user") || !strings.Contains(card, "did not get a message") {
		t.Fatalf("card %q", card)
	}
}

func TestEditDMOtherErrorAddsNoNote(t *testing.T) {
	e := newWz(t)
	td := e.add(wzGrp, "x", nil, "", 0)
	e.assignInGroup(td, "2")
	e.fs.ErrFn = func(chat int64, _ tg.OutMessage) error {
		if chat == wzBob {
			return &tg.APIError{Code: 500, Desc: "boom"}
		}
		return nil
	}
	e.save(wzGrp)
	if got := e.todoByID(wzGrp, td.ID); got.AssigneeID != wzBob {
		t.Fatalf("Save must succeed: %+v", got)
	}
	if card := e.lastEdit().Msg.Text; strings.Contains(card, "did not get a message") {
		t.Fatalf("no note for a transient error: %q", card)
	}
}

// ---- /edit N ----

func TestBareEditOpensWizard(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "milk", nil, "", 0)
	e.msg(wzAlice, wzAlice, "/edit "+e.lc(wzAlice).Digits("1"), 0)
	d := e.draft(wzAlice, wzAlice)
	if d.Data.EditTodoID != td.ID || d.Step != model.StepConfirm {
		t.Fatalf("draft %+v", d)
	}
	e.fs.Reset()
	e.msg(wzAlice, wzAlice, "/edit 99", 0)
	if len(e.fs.Messages()) != 1 {
		t.Fatalf("an unknown number gets one reply: %+v", e.fs.Messages())
	}
}

func TestEditWithTextStillEditsTextOnly(t *testing.T) {
	e := newWz(t)
	due := utcT(2026, 10, 14, 15, 0)
	td := e.add(wzAlice, "milk", &due, "daily 15:00", 0)
	e.msg(wzAlice, wzAlice, "/edit 1 oat milk", 0)
	e.noDraft(wzAlice, wzAlice)
	got := e.todoByID(wzAlice, td.ID)
	if got.Text != "oat milk" || got.Recur != "daily 15:00" || !got.DueAt.Equal(due) {
		t.Fatalf("item %+v", got)
	}
}

func TestEditAndNewReplaceEachOther(t *testing.T) {
	e := newWz(t)
	td := e.add(wzAlice, "milk", nil, "", 0)
	e.msg(wzAlice, wzAlice, "/new", 0)
	newCard := e.draft(wzAlice, wzAlice)
	e.pressEditBtn(wzAlice, wzAlice, td)
	if d := e.draft(wzAlice, wzAlice); d.Data.EditTodoID != td.ID || d.MsgID == newCard.MsgID {
		t.Fatalf("edit replaces new: %+v", d)
	}
	toast := e.press(wzAlice, wzAlice, newCard.MsgID, ui.Encode(ui.Action{Kind: ui.ActWizNav, ID: newCard.Version, Arg: ui.WizNavSave}))
	if toast != e.lc(wzAlice).T("wizard.toast.expired") {
		t.Fatalf("toast %q", toast)
	}
	e.msg(wzAlice, wzAlice, "/new", 0)
	if d := e.draft(wzAlice, wzAlice); d.Data.EditTodoID != 0 || d.Step != model.StepText {
		t.Fatalf("new replaces edit: %+v", d)
	}
}
