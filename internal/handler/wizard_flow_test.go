package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	_ "github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Wizard flow tests (#68). They run the handler on the real service and a
// SQLite database, and assert on stored drafts and items, and on the
// callback data of buttons built with ui.Encode, not on card text, so they
// hold whether or not ui.WizardCard is implemented yet.

const (
	wzAlice = int64(1)
	wzBob   = int64(2)
	wzCarol = int64(3)
	wzGrp   = int64(-100)
)

var wzNames = map[int64]string{wzAlice: "alice_u", wzBob: "bob_user", wzCarol: "carol_u"}

// countSvc counts Draft calls and can fail SaveDraft.
type countSvc struct {
	service.Service
	drafts   atomic.Int32
	saveFail atomic.Bool
}

func (c *countSvc) Draft(ctx context.Context, chat, user int64) (model.Draft, error) {
	c.drafts.Add(1)
	return c.Service.Draft(ctx, chat, user)
}

func (c *countSvc) SaveDraft(ctx context.Context, d model.Draft, a service.Actor, as int64, r nlparse.Result) (model.Todo, error) {
	if c.saveFail.Load() {
		return model.Todo{}, errors.New("disk full")
	}
	return c.Service.SaveDraft(ctx, d, a, as, r)
}

type wzEnv struct {
	t   *testing.T
	h   *Handler
	fs  *tgtest.FakeSender
	svc *service.Svc
	cs  *countSvc
	clk *clocktest.Fake
	seq int64
}

func newWz(t *testing.T) *wzEnv {
	t.Helper()
	u := url.URL{Scheme: "sqlite", Path: filepath.Join(t.TempDir(), "todo.db")}
	st, err := store.Open(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := clocktest.New(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)) // a Saturday
	svc := service.New(st, clk, service.Config{DefaultHour: 9, NodeID: "A"})
	svc.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cs := &countSvc{Service: svc}
	fs := &tgtest.FakeSender{}
	h := New(Deps{
		Svc: cs, Send: fs, Clock: clk, Parser: nlparse.Parser{DefaultHour: 9},
		Allowed:     map[int64]bool{wzAlice: true, wzBob: true, wzCarol: true},
		BotUsername: "TodoBot", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	e := &wzEnv{t: t, h: h, fs: fs, svc: svc, cs: cs, clk: clk}
	for _, id := range []int64{wzAlice, wzBob, wzCarol} { // everyone says hello, so names are known
		e.press(id, id, 1, "x")
	}
	fs.Reset()
	return e
}

func (e *wzEnv) msg(user, chat int64, text string, replyTo int) {
	e.t.Helper()
	kind := model.ChatPrivate
	if chat < 0 {
		kind = model.ChatGroup
	}
	e.seq++
	err := e.h.Handle(context.Background(), tg.Update{ID: e.seq, Message: &tg.Message{
		ID: int(1000 + e.seq), ChatID: chat, ChatKind: kind, FromID: user, FromUsername: wzNames[user],
		Text: text, ReplyToID: replyTo,
	}})
	if err != nil {
		e.t.Fatal(err)
	}
}

// press presses data on message msgID and returns the callback answer.
func (e *wzEnv) press(user, chat int64, msgID int, data string) string {
	e.t.Helper()
	e.seq++
	n := len(e.fs.Answers)
	err := e.h.Handle(context.Background(), tg.Update{ID: e.seq, Callback: &tg.Callback{
		ID: "cb", FromID: user, FromUsername: wzNames[user], ChatID: chat, MessageID: msgID, Data: data,
	}})
	if err != nil {
		e.t.Fatal(err)
	}
	if len(e.fs.Answers) != n+1 {
		e.t.Fatalf("callback answered %d times", len(e.fs.Answers)-n)
	}
	return e.fs.Answers[n].Text
}

func (e *wzEnv) draft(chat, user int64) model.Draft {
	e.t.Helper()
	d, err := e.svc.Draft(context.Background(), chat, user)
	if err != nil {
		e.t.Fatalf("draft of %d in %d: %v", user, chat, err)
	}
	return d
}

func (e *wzEnv) noDraft(chat, user int64) {
	e.t.Helper()
	if d, err := e.svc.Draft(context.Background(), chat, user); !errors.Is(err, service.ErrNotFound) {
		e.t.Fatalf("draft should be gone, got %+v, %v", d, err)
	}
}

// pressD presses a button of the draft's current version.
func (e *wzEnv) pressD(d model.Draft, user int64, kind ui.ActionKind, arg string) string {
	e.t.Helper()
	return e.press(user, d.ChatID, d.MsgID, ui.Encode(ui.Action{Kind: kind, ID: d.Version, Arg: arg}))
}

// seed stores a draft at step with data, owned by wzAlice in chat.
func (e *wzEnv) seed(chat int64, step model.DraftStep, data model.DraftData) model.Draft {
	e.t.Helper()
	d := model.Draft{ChatID: chat, UserID: wzAlice, MsgID: 77, Step: step, Data: data}
	if err := e.svc.StartDraft(context.Background(), &d); err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *wzEnv) todos(chat int64) []model.Todo {
	e.t.Helper()
	ts, _, err := e.svc.List(context.Background(), chat, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return ts
}

func (e *wzEnv) lastEdit() tgtest.Sent {
	e.t.Helper()
	ms := e.fs.Messages()
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Edit {
			return ms[i]
		}
	}
	e.t.Fatal("no edit")
	return tgtest.Sent{}
}

func (e *wzEnv) setLang(user int64, l i18n.Lang) {
	e.t.Helper()
	if err := e.svc.SetLang(context.Background(), user, l); err != nil {
		e.t.Fatal(err)
	}
}

func TestWizardStartCardIsPlaceholderAndGroupAsks(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/new", 0) // wzAlice has no keyboard yet, so the hook is armed
	sent := e.fs.Messages()
	first := sent[0]
	if first.Edit || len(first.Msg.ReplyKeyboard) != 0 ||
		len(first.Msg.Keyboard) != 1 || first.Msg.Keyboard[0][0].Data != "wz:0" {
		t.Fatalf("start card %+v", first)
	}
	d := e.draft(wzAlice, wzAlice)
	if d.Step != model.StepText || d.MsgID != first.MsgID || d.AskMsgID != 0 {
		t.Fatalf("draft %+v", d)
	}
	if !sent[len(sent)-1].Edit {
		t.Fatal("the card is edited to the first step")
	}

	e.fs.Reset()
	e.msg(wzAlice, wzGrp, "/new", 0)
	g := e.draft(wzGrp, wzAlice)
	if g.AskMsgID == 0 || g.AskMsgID == g.MsgID {
		t.Fatalf("group draft needs an ask message: %+v", g)
	}
	for _, s := range e.fs.Messages() {
		if len(s.Msg.ReplyKeyboard) != 0 {
			t.Fatalf("reply keyboard in a group: %+v", s)
		}
		if !s.Edit && s.MsgID == g.AskMsgID && !s.Msg.ForceReply {
			t.Fatal("the ask must be ForceReply")
		}
	}
}

func TestWizardEquivalentToAdd(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzGrp, "/new", 0)
	d := e.draft(wzGrp, wzAlice)
	e.msg(wzAlice, wzGrp, "call the bank", d.AskMsgID)
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepDate || d.Data.Text != "call the bank" {
		t.Fatalf("after text %+v", d)
	}
	e.pressD(d, wzAlice, ui.ActWizDate, "20261011") // tomorrow
	d = e.draft(wzGrp, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizTime, "1500")
	d = e.draft(wzGrp, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatWeekly)
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepAssignee {
		t.Fatalf("step %s", d.Step)
	}
	e.pressD(d, wzAlice, ui.ActWizAssign, "2")
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepConfirm || d.Data.AssigneeID != wzBob {
		t.Fatalf("confirm %+v", d)
	}
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	e.noDraft(wzGrp, wzAlice)

	e.msg(wzAlice, -200, "/add @bob_user call the bank tomorrow 15:00 every week", 0)
	got, want := e.todos(wzGrp), e.todos(-200)
	if len(got) != 1 || len(want) != 1 {
		t.Fatalf("items: wizard %d, add %d", len(got), len(want))
	}
	g, w := got[0], want[0]
	if g.Text != w.Text || g.Recur != w.Recur || g.AssigneeID != w.AssigneeID ||
		g.DueAt == nil || w.DueAt == nil || !g.DueAt.Equal(*w.DueAt) {
		t.Fatalf("wizard %+v\nadd    %+v", g, w)
	}
	if g.Recur != "weekly sun 15:00" || g.AssigneeID != wzBob {
		t.Fatalf("item %+v", g)
	}
	if last := e.lastEdit(); !strings.Contains(last.Msg.Text, "call the bank") || len(last.Msg.Keyboard) != 0 {
		t.Fatalf("added card %+v", last)
	}
}

func TestWizardSkipBackCancelFromEveryStep(t *testing.T) {
	data := model.DraftData{Text: "x", Date: "2026-10-12", Time: "10:00"}
	skip := map[model.DraftStep]model.DraftStep{
		model.StepDate: model.StepAssignee, model.StepTime: model.StepRepeat,
		model.StepRepeat: model.StepAssignee, model.StepAssignee: model.StepConfirm,
	}
	backTo := map[model.DraftStep]model.DraftStep{
		model.StepFound: model.StepText, model.StepDate: model.StepText, model.StepTime: model.StepDate,
		model.StepTimeText: model.StepTime, model.StepRepeat: model.StepTime, model.StepAssignee: model.StepRepeat,
		model.StepText: model.StepText, model.StepConfirm: model.StepConfirm,
	}
	for _, step := range []model.DraftStep{model.StepText, model.StepFound, model.StepDate, model.StepTime,
		model.StepTimeText, model.StepRepeat, model.StepAssignee, model.StepConfirm} {
		t.Run("skip "+string(step), func(t *testing.T) {
			e := newWz(t)
			d := e.seed(wzGrp, step, data)
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSkip)
			want, ok := skip[step]
			if !ok {
				want = step
			}
			if got := e.draft(wzGrp, wzAlice); got.Step != want {
				t.Fatalf("skip from %s -> %s, want %s", step, got.Step, want)
			}
		})
		t.Run("back "+string(step), func(t *testing.T) {
			e := newWz(t)
			d := e.seed(wzGrp, step, data)
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavBack)
			if got := e.draft(wzGrp, wzAlice); got.Step != backTo[step] {
				t.Fatalf("back from %s -> %s, want %s", step, got.Step, backTo[step])
			}
		})
		t.Run("cancel "+string(step), func(t *testing.T) {
			e := newWz(t)
			d := e.seed(wzGrp, step, data)
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavCancel)
			e.noDraft(wzGrp, wzAlice)
			if last := e.lastEdit(); len(last.Msg.Keyboard) != 0 || last.MsgID != 77 {
				t.Fatalf("cancelled card %+v", last)
			}
		})
	}
}

func TestWizardSkipStoresDefaults(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepAssignee, model.DraftData{Text: "x", AssigneeID: wzBob})
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSkip)
	if got := e.draft(wzGrp, wzAlice); got.Data.AssigneeID != 0 {
		t.Fatalf("group skip = nobody, got %d", got.Data.AssigneeID)
	}
	// Skip on the date step is "no date": time and repeat are cleared.
	d = e.seed(wzGrp, model.StepDate, model.DraftData{Text: "x", Date: "2026-10-12", Time: "10:00", Repeat: model.RepeatDaily})
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSkip)
	if got := e.draft(wzGrp, wzAlice); got.Data.Date != "" || got.Data.Time != "" || got.Data.Repeat != "" {
		t.Fatalf("no date left %+v", got.Data)
	}
}

func TestWizardBackFromAssigneeWithFoundDate(t *testing.T) {
	e := newWz(t)
	due := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	d := e.seed(wzGrp, model.StepAssignee, model.DraftData{Text: "milk", OrigText: "milk tomorrow 18:00", FoundText: "milk", FoundDue: &due, FoundTime: true})
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavBack)
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepFound {
		t.Fatalf("step %s", d.Step)
	}
	// No restores the whole text; Yes -> Back -> No keeps the phrase.
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavNoFound)
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepDate || d.Data.Text != "milk tomorrow 18:00" || d.Data.FoundDue != nil {
		t.Fatalf("after No: %+v", d)
	}
}

func TestWizardFoundDateFlow(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/new", 0)
	e.msg(wzAlice, wzAlice, "buy milk tomorrow 18:00", 0)
	d := e.draft(wzAlice, wzAlice)
	if d.Step != model.StepFound || d.Data.FoundText != "buy milk" || d.Data.FoundDue == nil || !d.Data.FoundTime {
		t.Fatalf("found %+v", d)
	}
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavUseFound)
	d = e.draft(wzAlice, wzAlice)
	// Private chat with other members: assignee is asked.
	if d.Step != model.StepAssignee || d.Data.Text != "buy milk" || d.Data.Date != "" {
		t.Fatalf("after Yes %+v", d)
	}
	e.pressD(d, wzAlice, ui.ActWizAssign, ui.WizAssignMe)
	d = e.draft(wzAlice, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	ts := e.todos(wzAlice)
	if len(ts) != 1 || ts[0].Text != "buy milk" || ts[0].DueAt == nil ||
		!ts[0].DueAt.Equal(time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)) || ts[0].AssigneeID != 0 {
		t.Fatalf("items %+v", ts)
	}
}

func TestWizardEditFromConfirm(t *testing.T) {
	e := newWz(t)
	data := model.DraftData{Text: "x", OrigText: "x", Date: "2026-10-12", Time: "10:00"}
	for arg, want := range map[string]model.DraftStep{
		ui.WizEditText: model.StepText, ui.WizEditDate: model.StepDate, ui.WizEditTime: model.StepTime,
		ui.WizEditRepeat: model.StepRepeat, ui.WizEditAssignee: model.StepAssignee,
	} {
		d := e.seed(wzGrp, model.StepConfirm, data)
		e.pressD(d, wzAlice, ui.ActWizEdit, arg)
		got := e.draft(wzGrp, wzAlice)
		if got.Step != want || !got.Data.Editing {
			t.Fatalf("edit %s -> %+v", arg, got)
		}
		// Back from an edited step returns to the summary.
		e.pressD(got, wzAlice, ui.ActWizNav, ui.WizNavBack)
		if back := e.draft(wzGrp, wzAlice); back.Step != model.StepConfirm || back.Data.Editing {
			t.Fatalf("back after edit %s: %+v", arg, back)
		}
	}
	// The menu itself is a view: no write.
	d := e.seed(wzGrp, model.StepConfirm, data)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavEdit)
	if got := e.draft(wzGrp, wzAlice); got.Version != d.Version {
		t.Fatal("opening the edit menu wrote the draft")
	}
	// Time and repeat need a date: without one the date comes first.
	d = e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "x"})
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditTime)
	if got := e.draft(wzGrp, wzAlice); got.Step != model.StepDate {
		t.Fatalf("edit time without date -> %s", got.Step)
	}
}

func TestWizardEditAnswersReturnToConfirm(t *testing.T) {
	e := newWz(t)
	due := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	d := e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "milk", OrigText: "milk tomorrow", FoundText: "milk", FoundDue: &due})
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditText)
	d = e.draft(wzGrp, wzAlice)
	e.msg(wzAlice, wzGrp, "eggs", d.MsgID)
	got := e.draft(wzGrp, wzAlice)
	// A text edit does not detect the date again, and keeps the chosen date.
	if got.Step != model.StepConfirm || got.Data.Text != "eggs" || got.Data.OrigText != "eggs" ||
		got.Data.Editing || got.Data.FoundDue == nil {
		t.Fatalf("after text edit %+v", got)
	}
	// A new date walks on through time and repeat, then back to confirm.
	e.pressD(got, wzAlice, ui.ActWizEdit, ui.WizEditDate)
	got = e.draft(wzGrp, wzAlice)
	e.pressD(got, wzAlice, ui.ActWizDate, "20261020")
	got = e.draft(wzGrp, wzAlice)
	if got.Step != model.StepTime || got.Data.Date != "2026-10-20" || got.Data.FoundDue != nil {
		t.Fatalf("after date edit %+v", got)
	}
	e.pressD(got, wzAlice, ui.ActWizTime, "1800")
	got = e.draft(wzGrp, wzAlice)
	if got.Step != model.StepRepeat || got.Data.Time != "18:00" {
		t.Fatalf("after time %+v", got)
	}
	e.pressD(got, wzAlice, ui.ActWizRepeat, ui.WizRepeatOnce)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
	if got = e.draft(wzGrp, wzAlice); got.Step != model.StepConfirm || got.Data.Editing {
		t.Fatalf("after assignee %+v", got)
	}
}

func TestWizardEditRepeatOfNoDateItem(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "gym", OrigText: "gym"})
	e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditRepeat)
	d = e.draft(wzGrp, wzAlice)
	e.pressD(d, wzAlice, ui.ActWizDate, "20261012")
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizTime, "1800")
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepRepeat {
		t.Fatalf("step %s", d.Step)
	}
	e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatWeekly)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
	ts := e.todos(wzGrp)
	if len(ts) != 1 || ts[0].Recur != "weekly mon 18:00" {
		t.Fatalf("items %+v", ts)
	}
}

func TestWizardFoundRuleSurvivesDateEdit(t *testing.T) {
	for _, edit := range []string{ui.WizEditRepeat, ui.WizEditDate} {
		e := newWz(t)
		e.msg(wzAlice, wzGrp, "/new", 0)
		d := e.draft(wzGrp, wzAlice)
		e.msg(wzAlice, wzGrp, "gym every monday 18:00", d.AskMsgID)
		d = e.draft(wzGrp, wzAlice)
		if d.Step != model.StepFound || d.Data.FoundRule == "" {
			t.Fatalf("found %+v", d)
		}
		e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavUseFound)
		e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
		d = e.draft(wzGrp, wzAlice)
		e.pressD(d, wzAlice, ui.ActWizEdit, edit)
		d = e.draft(wzGrp, wzAlice)
		e.pressD(d, wzAlice, ui.ActWizDate, "20261019") // a Monday
		e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizTime, "1800")
		d = e.draft(wzGrp, wzAlice)
		if d.Step != model.StepRepeat || d.Data.Repeat != model.RepeatWeekly {
			t.Fatalf("%s: repeat step %+v", edit, d)
		}
		e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatWeekly) // keep the pre-selected choice
		e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
		e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
		ts := e.todos(wzGrp)
		if len(ts) != 1 || ts[0].Recur != "weekly mon 18:00" {
			t.Fatalf("%s: items %+v", edit, ts)
		}
	}
}

func TestWizardDoubleSaveAndCancelKeepFinalCard(t *testing.T) {
	for _, finish := range []string{ui.WizNavSave, ui.WizNavCancel} {
		for _, second := range []string{"sequential", "other node"} {
			e := newWz(t)
			d := e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "milk"})
			e.pressD(d, wzAlice, ui.ActWizNav, finish)
			final := e.lastEdit()
			e.fs.Reset()
			h := e.h
			if second == "other node" {
				h = New(Deps{Svc: e.svc, Send: e.fs, Clock: e.clk, Parser: e.h.Parser, Allowed: e.h.Allowed, BotUsername: "TodoBot"})
			}
			e.seq++
			err := h.Handle(context.Background(), tg.Update{ID: e.seq, Callback: &tg.Callback{
				ID: "cb2", FromID: wzAlice, ChatID: wzGrp, MessageID: d.MsgID,
				Data: ui.Encode(ui.Action{Kind: ui.ActWizNav, ID: d.Version, Arg: finish}),
			}})
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range e.fs.Messages() {
				if m.Edit {
					t.Fatalf("%s/%s: second press edited the final card %q (was %q)", finish, second, m.Msg.Text, final.Msg.Text)
				}
			}
			if len(e.fs.Answers) != 1 || e.fs.Answers[0].Text == "" {
				t.Fatalf("%s/%s: want one toast, got %+v", finish, second, e.fs.Answers)
			}
			if finish == ui.WizNavSave && len(e.todos(wzGrp)) != 1 {
				t.Fatalf("items %d", len(e.todos(wzGrp)))
			}
		}
	}
}

func TestWizardExpiredStaleAndForeign(t *testing.T) {
	e := newWz(t)
	lc := i18n.Locale{Lang: i18n.En}
	d := e.seed(wzGrp, model.StepDate, model.DraftData{Text: "x"})

	if got := e.pressD(d, wzBob, ui.ActWizNav, ui.WizNavSkip); got != lc.T("wizard.toast.foreign") {
		t.Fatalf("foreign toast %q", got)
	}
	if e.draft(wzGrp, wzAlice).Version != d.Version {
		t.Fatal("another member's press changed the draft")
	}

	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSkip) // version moves on
	cur := e.draft(wzGrp, wzAlice)
	e.fs.Reset()
	if got := e.press(wzAlice, wzGrp, d.MsgID, ui.Encode(ui.Action{Kind: ui.ActWizNav, ID: d.Version, Arg: ui.WizNavSkip})); got != lc.T("wizard.toast.stale") {
		t.Fatalf("stale toast %q", got)
	}
	if after := e.draft(wzGrp, wzAlice); after.Version != cur.Version || after.Step != cur.Step {
		t.Fatalf("stale press changed the draft: %+v -> %+v", cur, after)
	}
	if len(e.fs.Messages()) != 1 || !e.fs.Messages()[0].Edit {
		t.Fatalf("stale press must redraw the card: %+v", e.fs.Messages())
	}

	e.clk.Advance(31 * time.Minute)
	e.fs.Reset()
	if got := e.pressD(cur, wzAlice, ui.ActWizNav, ui.WizNavSkip); got != lc.T("wizard.toast.expired") {
		t.Fatalf("expired toast %q", got)
	}
	for _, m := range e.fs.Messages() {
		if m.Edit {
			t.Fatalf("a missing draft must not edit the card: %+v", m)
		}
	}
	// A card that never had a draft is expired too.
	if got := e.press(wzAlice, wzGrp, 999, ui.Encode(ui.Action{Kind: ui.ActWizNav, ID: 5, Arg: ui.WizNavSkip})); got != lc.T("wizard.toast.expired") {
		t.Fatalf("unknown card toast %q", got)
	}
}

func TestWizardViewsDoNotExtendExpiry(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepDate, model.DraftData{Text: "x"})
	e.clk.Advance(20 * time.Minute)
	e.pressD(d, wzAlice, ui.ActWizDate, ui.WizDateGrid)
	e.pressD(d, wzAlice, ui.ActWizMonth, ui.MonthArg(calendar.Gregorian, 2026, 11))
	e.clk.Advance(11 * time.Minute)
	e.noDraft(wzGrp, wzAlice)
}

func TestWizardClampsDatesAndMonths(t *testing.T) {
	e := newWz(t)
	lc := i18n.Locale{Lang: i18n.En}
	d := e.seed(wzGrp, model.StepDate, model.DraftData{Text: "x"})
	for _, arg := range []string{"20261009", "20311011", "21001231"} {
		got := e.pressD(d, wzAlice, ui.ActWizDate, arg)
		if after := e.draft(wzGrp, wzAlice); after.Version != d.Version || after.Data.Date != "" {
			t.Fatalf("date %s accepted: %+v", arg, after)
		}
		if arg == "20261009" && got != lc.T("wizard.toast.past") {
			t.Fatalf("past toast %q", got)
		}
	}
	// 5 years ahead is the last day allowed.
	e.pressD(d, wzAlice, ui.ActWizDate, "20311010")
	if after := e.draft(wzGrp, wzAlice); after.Data.Date != "2031-10-10" {
		t.Fatalf("edge date refused: %+v", after)
	}

	d = e.seed(wzGrp, model.StepDate, model.DraftData{Text: "x"})
	e.fs.Reset()
	for _, arg := range []string{
		ui.MonthArg(calendar.Gregorian, 2026, 9),  // before this month
		ui.MonthArg(calendar.Gregorian, 2031, 11), // past the window
		ui.MonthArg(calendar.Jalali, 1405, 7),     // not the user's calendar
	} {
		e.pressD(d, wzAlice, ui.ActWizMonth, arg)
	}
	if n := len(e.fs.Messages()); n != 0 {
		t.Fatalf("out-of-window months redrew the card %d times", n)
	}
	e.pressD(d, wzAlice, ui.ActWizMonth, ui.MonthArg(calendar.Gregorian, 2031, 10))
	if n := len(e.fs.Messages()); n != 1 {
		t.Fatalf("last month of the window must be shown (%d edits)", n)
	}
}

func TestWizardAssigneeChecks(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepAssignee, model.DraftData{Text: "x"})
	// Not allowed, not a member, nobody: all leave the draft alone.
	delete(e.h.Allowed, wzCarol)
	for _, arg := range []string{"3", "99", "7"} {
		e.pressD(d, wzAlice, ui.ActWizAssign, arg)
		if got := e.draft(wzGrp, wzAlice); got.Version != d.Version || got.Data.AssigneeID != 0 {
			t.Fatalf("assignee %s accepted: %+v", arg, got)
		}
	}
	// "Nobody" does not exist in a private chat.
	p := e.seed(wzAlice, model.StepAssignee, model.DraftData{Text: "x"})
	e.pressD(p, wzAlice, ui.ActWizAssign, ui.WizAssignNone)
	if got := e.draft(wzAlice, wzAlice); got.Version != p.Version {
		t.Fatalf("nobody accepted in private: %+v", got)
	}

	// Save checks again: wzBob leaves the allowlist after being chosen.
	d = e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "x", AssigneeID: wzBob})
	delete(e.h.Allowed, wzBob)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	got := e.draft(wzGrp, wzAlice)
	if got.Step != model.StepAssignee || got.Data.AssigneeID != 0 || len(e.todos(wzGrp)) != 0 {
		t.Fatalf("save with a removed member: %+v", got)
	}
}

func TestWizardNobodyAndMe(t *testing.T) {
	cases := []struct {
		name   string
		chat   int64
		arg    string
		wantID int64
	}{
		{"group nobody", wzGrp, ui.WizAssignNone, 0},
		{"group me", wzGrp, ui.WizAssignMe, wzAlice},
		{"private me", wzAlice, ui.WizAssignMe, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newWz(t)
			d := e.seed(c.chat, model.StepAssignee, model.DraftData{Text: "task"})
			e.pressD(d, wzAlice, ui.ActWizAssign, c.arg)
			d = e.draft(c.chat, wzAlice)
			wantStored := int64(0)
			if c.arg == ui.WizAssignMe {
				wantStored = wzAlice
			}
			if d.Step != model.StepConfirm || d.Data.AssigneeID != wantStored {
				t.Fatalf("stored %+v", d)
			}
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
			ts := e.todos(c.chat)
			if len(ts) != 1 || ts[0].AssigneeID != c.wantID {
				t.Fatalf("items %+v, want assignee %d", ts, c.wantID)
			}
		})
	}
}

func TestWizardPrivateWithoutMembersSkipsAssignee(t *testing.T) {
	e := newWz(t)
	delete(e.h.Allowed, wzBob)
	delete(e.h.Allowed, wzCarol)
	d := e.seed(wzAlice, model.StepRepeat, model.DraftData{Text: "x", Date: "2026-10-12"})
	e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatOnce)
	if got := e.draft(wzAlice, wzAlice); got.Step != model.StepConfirm || got.Data.AssigneeID != wzAlice {
		t.Fatalf("got %+v", got)
	}
}

func TestWizardTextAnswers(t *testing.T) {
	e := newWz(t)
	// Private: plain text on a text step is the answer, not an item.
	e.msg(wzAlice, wzAlice, "/new", 0)
	e.msg(wzAlice, wzAlice, "pay rent", 0)
	if d := e.draft(wzAlice, wzAlice); d.Data.Text != "pay rent" || d.Step != model.StepDate {
		t.Fatalf("draft %+v", d)
	}
	if n := len(e.todos(wzAlice)); n != 0 {
		t.Fatalf("answer became an item (%d)", n)
	}
	// On a button step, plain text is an item as before.
	e.msg(wzAlice, wzAlice, "buy tea", 0)
	if ts := e.todos(wzAlice); len(ts) != 1 || ts[0].Text != "buy tea" {
		t.Fatalf("items %+v", ts)
	}
	// After Cancel plain text is an item again.
	e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavCancel)
	e.msg(wzAlice, wzAlice, "buy jam", 0)
	if n := len(e.todos(wzAlice)); n != 2 {
		t.Fatalf("after cancel: %d items", n)
	}
}

func TestWizardEmptyAndTooLongText(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/new", 0)
	v := e.draft(wzAlice, wzAlice).Version
	e.msg(wzAlice, wzAlice, "   ", 0)
	e.msg(wzAlice, wzAlice, strings.Repeat("x", maxTodoChars+1), 0)
	if d := e.draft(wzAlice, wzAlice); d.Step != model.StepText || d.Version != v || d.Data.Text != "" {
		t.Fatalf("draft %+v", d)
	}
}

func TestWizardGroupIgnoresNonReplies(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzGrp, "/new", 0)
	d := e.draft(wzGrp, wzAlice)
	e.fs.Reset()
	before := e.cs.drafts.Load()
	e.msg(wzAlice, wzGrp, "just chatting", 0) // not a reply: no query at all
	if e.cs.drafts.Load() != before {
		t.Fatal("a non-reply group message queried the draft")
	}
	e.msg(wzAlice, wzGrp, "a reply to someone else", 4242) // a reply, but not to the wizard
	e.msg(wzBob, wzGrp, "wzBob replies to the card", d.MsgID)
	if got := e.draft(wzGrp, wzAlice); got.Version != d.Version || got.Data.Text != "" {
		t.Fatalf("draft changed: %+v", got)
	}
	if len(e.fs.Messages()) != 0 || len(e.todos(wzGrp)) != 0 {
		t.Fatalf("something was sent or added: %+v", e.fs.Messages())
	}
	e.msg(wzAlice, wzGrp, "a reply to the card", d.MsgID) // the card works as well as the ask
	if got := e.draft(wzGrp, wzAlice); got.Data.Text != "a reply to the card" {
		t.Fatalf("draft %+v", got)
	}
}

func TestWizardReplyKeyboardLabelStarts(t *testing.T) {
	for _, l := range []i18n.Lang{i18n.En, i18n.Fa} {
		e := newWz(t)
		e.msg(wzAlice, wzAlice, ui.NewTaskLabel(i18n.Locale{Lang: l}), 0)
		d := e.draft(wzAlice, wzAlice)
		if d.Step != model.StepText {
			t.Fatalf("%s: %+v", l, d)
		}
		if n := len(e.todos(wzAlice)); n != 0 {
			t.Fatalf("%s: the label became an item", l)
		}
		// In a group the label is just text.
		e.msg(wzAlice, wzGrp, ui.NewTaskLabel(i18n.Locale{Lang: l}), 0)
		e.noDraft(wzGrp, wzAlice)
	}
	// A Farsi user pressing the English label (sent before /lang) also starts it.
	e := newWz(t)
	e.setLang(wzAlice, i18n.Fa)
	e.msg(wzAlice, wzAlice, ui.NewTaskLabel(i18n.Locale{Lang: i18n.En}), 0)
	e.draft(wzAlice, wzAlice)
}

func TestWizardTypedTime(t *testing.T) {
	good := map[string][2]int{"15:30": {15, 30}, "3pm": {15, 0}, "3:30 PM": {15, 30}, "۱۵:۳۰": {15, 30},
		"12am": {0, 0}, "12pm": {12, 0}, "9": {9, 0}, "07.05": {7, 5}}
	for in, want := range good {
		if hh, mm, ok := parseTypedTime(in); !ok || hh != want[0] || mm != want[1] {
			t.Errorf("%q -> %d:%d %v", in, hh, mm, ok)
		}
	}
	for _, in := range []string{"", "25:00", "13pm", "0pm", "10:99", "noon", "3 o'clock"} {
		if _, _, ok := parseTypedTime(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestWizardTimeTextStep(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepTime, model.DraftData{Text: "x", Date: "2026-10-12"})
	e.pressD(d, wzAlice, ui.ActWizTime, ui.WizTimeType)
	d = e.draft(wzGrp, wzAlice)
	if d.Step != model.StepTimeText || d.AskMsgID == 0 {
		t.Fatalf("group time_text needs an ask: %+v", d)
	}
	e.msg(wzAlice, wzGrp, "later", d.AskMsgID)
	if got := e.draft(wzGrp, wzAlice); got.Step != model.StepTimeText || got.Version != d.Version {
		t.Fatalf("bad time changed the draft: %+v", got)
	}
	e.msg(wzAlice, wzGrp, "۱۵:۳۰", d.AskMsgID)
	if got := e.draft(wzGrp, wzAlice); got.Step != model.StepRepeat || got.Data.Time != "15:30" {
		t.Fatalf("typed time %+v", got)
	}
}

func TestWizardSaveRules(t *testing.T) {
	e := newWz(t) // Saturday 2026-10-10 12:00 UTC, default time 09:00
	cases := []struct {
		name     string
		data     model.DraftData
		wantStep model.DraftStep // "" = saved
		due      time.Time
		hasTime  bool
		recur    string
	}{
		{"future time", model.DraftData{Text: "x", Date: "2026-10-12", Time: "15:00"}, "", time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC), true, ""},
		{"today past explicit time", model.DraftData{Text: "x", Date: "2026-10-10", Time: "08:00"}, model.StepTime, time.Time{}, false, ""},
		{"today default after slot", model.DraftData{Text: "x", Date: "2026-10-10"}, "", time.Date(2026, 10, 10, 23, 59, 0, 0, time.UTC), false, ""},
		{"past day", model.DraftData{Text: "x", Date: "2026-10-09", Time: "08:00"}, model.StepDate, time.Time{}, false, ""},
		{"repeating past time advances", model.DraftData{Text: "x", Date: "2026-10-10", Time: "08:00", Repeat: model.RepeatDaily}, "", time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC), true, "daily 08:00"},
		{"monthly", model.DraftData{Text: "x", Date: "2026-10-20", Time: "08:00", Repeat: model.RepeatMonthly}, "", time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), true, "monthly 20 08:00"},
		{"yearly", model.DraftData{Text: "x", Date: "2026-12-25", Repeat: model.RepeatYearly}, "", time.Date(2026, 12, 25, 9, 0, 0, 0, time.UTC), false, "yearly 12-25 09:00"},
		{"no date", model.DraftData{Text: "x"}, "", time.Time{}, false, ""},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chat := -300 - int64(i)
			d := e.seed(chat, model.StepConfirm, c.data)
			e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
			if c.wantStep != "" {
				got := e.draft(chat, wzAlice)
				if got.Step != c.wantStep || !got.Data.Editing || len(e.todos(chat)) != 0 {
					t.Fatalf("got %+v", got)
				}
				return
			}
			e.noDraft(chat, wzAlice)
			ts := e.todos(chat)
			if len(ts) != 1 || ts[0].Recur != c.recur {
				t.Fatalf("items %+v", ts)
			}
			if c.due.IsZero() != (ts[0].DueAt == nil) || (!c.due.IsZero() && !ts[0].DueAt.Equal(c.due)) {
				t.Fatalf("due %v, want %v", ts[0].DueAt, c.due)
			}
		})
	}
}

func TestWizardBuildResultJalali(t *testing.T) {
	h := New(Deps{Parser: nlparse.Parser{DefaultHour: 9}})
	loc := time.UTC
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, loc)
	lc := i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}
	// 2026-12-25 is 4 Dey 1405.
	res, problem, err := h.buildResult(model.Draft{Data: model.DraftData{Text: "x", Date: "2026-12-25", Time: "15:00", Repeat: model.RepeatYearly}}, lc, loc, now)
	if err != nil || problem != saveOK || res.Rule == nil || res.Rule.String() != "yearly 10-04 15:00 jalali" {
		t.Fatalf("%v %v %+v", err, problem, res.Rule)
	}
	res, _, err = h.buildResult(model.Draft{Data: model.DraftData{Text: "x", Date: "2026-12-25", Time: "15:00", Repeat: model.RepeatMonthly}}, lc, loc, now)
	if err != nil || res.Rule.String() != "monthly 4 15:00 jalali" {
		t.Fatalf("%v %+v", err, res.Rule)
	}
	// Weekly follows the Gregorian weekday of the date (a Friday).
	res, _, err = h.buildResult(model.Draft{Data: model.DraftData{Text: "x", Date: "2026-12-25", Repeat: model.RepeatWeekly}}, lc, loc, now)
	if err != nil || res.Rule.String() != "weekly fri 09:00" || res.HasTime {
		t.Fatalf("%v %+v", err, res.Rule)
	}
}

func TestWizardSaveOnceAndFailure(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "x"})
	e.cs.saveFail.Store(true)
	if got := e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave); got == "" {
		t.Fatal("a failed save must tell the user")
	}
	if got := e.draft(wzGrp, wzAlice); got.Version != d.Version || len(e.todos(wzGrp)) != 0 {
		t.Fatalf("failed save changed things: %+v", got)
	}
	e.cs.saveFail.Store(false)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave) // double press
	if n := len(e.todos(wzGrp)); n != 1 {
		t.Fatalf("%d items", n)
	}
}

func TestWizardSecondNodeContinues(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzGrp, "/new", 0)
	d := e.draft(wzGrp, wzAlice)
	// Another handler instance on the same database continues the wizard.
	h2 := New(Deps{Svc: e.svc, Send: e.fs, Clock: e.clk, Parser: e.h.Parser, Allowed: e.h.Allowed, BotUsername: "TodoBot"})
	err := h2.Handle(context.Background(), tg.Update{ID: 900, Message: &tg.Message{ID: 5, ChatID: wzGrp, ChatKind: model.ChatGroup,
		FromID: wzAlice, Text: "from node two", ReplyToID: d.AskMsgID}})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.draft(wzGrp, wzAlice); got.Data.Text != "from node two" {
		t.Fatalf("%+v", got)
	}
}

// ---- the reply keyboard ----

func TestReplyKeyboardOnceAndNotAgain(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/tz", 0)
	first, _ := e.fs.Last()
	want := ui.NewTaskKeyboard(i18n.Locale{Lang: i18n.En})
	if len(first.Msg.ReplyKeyboard) != 1 || first.Msg.ReplyKeyboard[0][0] != want[0][0] {
		t.Fatalf("first reply %+v", first.Msg)
	}
	if p, _ := e.svc.Prefs(context.Background(), wzAlice, ""); p.KBVersion != ui.NewTaskKeyboardVersion {
		t.Fatalf("KBVersion %d", p.KBVersion)
	}
	e.msg(wzAlice, wzAlice, "/tz", 0)
	if second, _ := e.fs.Last(); len(second.Msg.ReplyKeyboard) != 0 {
		t.Fatalf("second reply carries it again: %+v", second.Msg)
	}
	// /help always carries it, even when the version matches.
	e.msg(wzAlice, wzAlice, "/help", 0)
	if help, _ := e.fs.Last(); len(help.Msg.ReplyKeyboard) != 1 {
		t.Fatalf("/help %+v", help.Msg)
	}
}

func TestReplyKeyboardNeverInGroupsOrOnInlineReplies(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzGrp, "/tz", 0)
	e.msg(wzAlice, wzGrp, "/help", 0)
	e.msg(wzAlice, wzGrp, "/new", 0)
	for _, s := range e.fs.Messages() {
		if len(s.Msg.ReplyKeyboard) != 0 {
			t.Fatalf("group message with a reply keyboard: %+v", s)
		}
	}
	// /list in a private chat has inline buttons, so it is skipped and the
	// version stays unset; the next plain reply carries the keyboard.
	e.fs.Reset()
	e.msg(wzCarol, wzCarol, "/list", 0)
	if l, _ := e.fs.Last(); len(l.Msg.ReplyKeyboard) != 0 || len(l.Msg.Keyboard) == 0 {
		t.Fatalf("/list %+v", l.Msg)
	}
	if p, _ := e.svc.Prefs(context.Background(), wzCarol, ""); p.KBVersion != 0 {
		t.Fatalf("KBVersion %d after an inline reply", p.KBVersion)
	}
	e.msg(wzCarol, wzCarol, "/tz", 0)
	if l, _ := e.fs.Last(); len(l.Msg.ReplyKeyboard) != 1 {
		t.Fatalf("/tz %+v", l.Msg)
	}
}

func TestReplyKeyboardAfterLang(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/tz", 0) // has it now
	e.msg(wzAlice, wzAlice, "/lang fa", 0)
	l, _ := e.fs.Last()
	fa := ui.NewTaskKeyboard(i18n.Locale{Lang: i18n.Fa})
	if len(l.Msg.ReplyKeyboard) != 1 || l.Msg.ReplyKeyboard[0][0] != fa[0][0] {
		t.Fatalf("/lang fa %+v", l.Msg)
	}
	// The language buttons answer by an edit, so the version is reset.
	e.press(wzAlice, wzAlice, 5, ui.Encode(ui.Action{Kind: ui.ActSetLang, Arg: "en"}))
	if p, _ := e.svc.Prefs(context.Background(), wzAlice, ""); p.KBVersion != 0 {
		t.Fatalf("KBVersion %d after the language button", p.KBVersion)
	}
	e.msg(wzAlice, wzAlice, "/tz", 0)
	en := ui.NewTaskKeyboard(i18n.Locale{Lang: i18n.En})
	if l, _ = e.fs.Last(); len(l.Msg.ReplyKeyboard) != 1 || l.Msg.ReplyKeyboard[0][0] != en[0][0] {
		t.Fatalf("after the button %+v", l.Msg)
	}
	// A group's language button does not touch the version.
	e.press(wzAlice, wzGrp, 6, ui.Encode(ui.Action{Kind: ui.ActSetLang, Arg: "fa"}))
	if p, _ := e.svc.Prefs(context.Background(), wzAlice, ""); p.KBVersion != ui.NewTaskKeyboardVersion {
		t.Fatalf("KBVersion %d after a group button", p.KBVersion)
	}
}

func TestWizardEntryPoints(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzAlice, "/add", 0) // bare /add
	e.draft(wzAlice, wzAlice)
	e.pressD(e.draft(wzAlice, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavCancel)
	e.noDraft(wzAlice, wzAlice)

	e.press(wzBob, wzGrp, 12, ui.Encode(ui.Action{Kind: ui.ActWizStart})) // the button under /list
	if d := e.draft(wzGrp, wzBob); d.Step != model.StepText {
		t.Fatalf("%+v", d)
	}
	// /new replaces an earlier draft; the old card then says "expired".
	old := e.draft(wzGrp, wzBob)
	e.msg(wzBob, wzGrp, "/new", 0)
	if cur := e.draft(wzGrp, wzBob); cur.MsgID == old.MsgID {
		t.Fatal("not replaced")
	}
	if got := e.press(wzBob, wzGrp, old.MsgID, ui.Encode(ui.Action{Kind: ui.ActWizNav, ID: old.Version, Arg: ui.WizNavCancel})); got == "" {
		t.Fatal("the old card must say expired")
	}
	// The past-day cell only answers.
	if got := e.press(wzAlice, wzGrp, 3, "wz:0:p"); got == "" {
		t.Fatal("past day needs a toast")
	}
}

func TestListHasNewTaskButton(t *testing.T) {
	e := newWz(t)
	e.msg(wzAlice, wzGrp, "/list", 0)
	l, _ := e.fs.Last()
	rows := l.Msg.Keyboard
	if len(rows) == 0 || rows[len(rows)-1][0].Data != ui.NewTaskButton(i18n.Locale{Lang: i18n.En}).Data {
		t.Fatalf("keyboard %+v", rows)
	}
}
