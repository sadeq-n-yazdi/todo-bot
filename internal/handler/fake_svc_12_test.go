package handler

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// fakeSvc12 is an in-memory service.Service for the #12 tests. Its state
// lives in the struct, so a second Handler built on the same fakeSvc12
// behaves like a restarted process talking to the same database.
type fakeSvc12 struct {
	mu        sync.Mutex
	clk       *clocktest.Fake
	promptTTL time.Duration
	prefs     map[int64]model.Prefs
	todos     []*model.Todo
	reminders map[int64]*model.Reminder
	prompts   map[[2]int64]model.Prompt
	digests   map[int64]string
	migrated  [][2]int64
	remindAt  []time.Time
	nextID    int64
	seen      map[int64]bool
	calls     []string
	emptyText string // Edit/AddTodo return ErrEmptyText for this text
	takeErr   error  // returned by TakePrompt when set
}

var _ service.Service = (*fakeSvc12)(nil)

func newFakeSvc12(clk *clocktest.Fake) *fakeSvc12 {
	return &fakeSvc12{
		clk:       clk,
		promptTTL: 10 * time.Minute,
		prefs:     map[int64]model.Prefs{},
		reminders: map[int64]*model.Reminder{},
		prompts:   map[[2]int64]model.Prompt{},
		digests:   map[int64]string{},
		seen:      map[int64]bool{},
	}
}

// addTodo seeds an open todo and returns it.
func (f *fakeSvc12) addTodo(chatID int64, text string) *model.Todo {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	num := 1
	for _, t := range f.todos {
		if t.ChatID == chatID && t.Num >= num {
			num = t.Num + 1
		}
	}
	td := &model.Todo{ID: f.nextID, ChatID: chatID, Num: num, Text: text, Status: model.TodoOpen}
	f.todos = append(f.todos, td)
	return td
}

func (f *fakeSvc12) addReminder(chatID, todoID int64) *model.Reminder {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	r := &model.Reminder{ID: f.nextID, TodoID: todoID, ChatID: chatID, FireAt: f.clk.Now(), Kind: model.ReminderDue, State: model.ReminderSent}
	f.reminders[r.ID] = r
	return r
}

func (f *fakeSvc12) todo(id int64) model.Todo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.find(id)
}

func (f *fakeSvc12) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeSvc12) rec(name string) { f.calls = append(f.calls, name) }

func (f *fakeSvc12) find(id int64) *model.Todo {
	for _, t := range f.todos {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (f *fakeSvc12) Prefs(_ context.Context, userID int64, tgLang string) (model.Prefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prefs[userID]
	if !ok {
		p = model.Prefs{TZ: time.UTC, Locale: i18n.Default(tgLang)}
		f.prefs[userID] = p
	}
	return p, nil
}

func (f *fakeSvc12) SetLang(_ context.Context, userID int64, lang i18n.Lang) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetLang")
	if !lang.Valid() {
		return service.ErrBadLang
	}
	p := f.prefs[userID]
	p.Locale.Lang = lang
	f.prefs[userID] = p
	return nil
}

func (f *fakeSvc12) SetCalendar(_ context.Context, userID int64, cal calendar.Kind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetCalendar")
	if !cal.Valid() {
		return service.ErrBadCalendar
	}
	p := f.prefs[userID]
	p.Locale.Cal = cal
	f.prefs[userID] = p
	return nil
}

func (f *fakeSvc12) AddTodo(_ context.Context, a service.Actor, r nlparse.Result) (model.Todo, error) {
	if f.emptyText != "" && r.Text == f.emptyText {
		return model.Todo{}, service.ErrEmptyText
	}
	if r.Due != nil && r.Due.Before(f.clk.Now()) {
		return model.Todo{}, service.ErrInPast
	}
	td := f.addTodo(a.ChatID, r.Text)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("AddTodo")
	td.DueAt = r.Due
	return *td, nil
}

func (f *fakeSvc12) open(chatID int64) []model.Todo {
	var out []model.Todo
	for _, t := range f.todos {
		if t.ChatID == chatID && t.Status == model.TodoOpen {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

func (f *fakeSvc12) List(_ context.Context, chatID int64, page int) ([]model.Todo, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.open(chatID)
	pages := max(1, (len(all)+ui.PageSize-1)/ui.PageSize)
	lo := min(page*ui.PageSize, len(all))
	hi := min(lo+ui.PageSize, len(all))
	return all[lo:hi], pages, nil
}

func (f *fakeSvc12) ByNum(_ context.Context, chatID int64, num int) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.open(chatID) {
		if t.Num == num {
			return t, nil
		}
	}
	return model.Todo{}, service.ErrNotFound
}

func (f *fakeSvc12) Todo(_ context.Context, a service.Actor, id int64) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("Todo")
	t, err := f.own(a, id)
	if err != nil {
		return model.Todo{}, err
	}
	return *t, nil
}

// own returns the todo if it belongs to the actor's chat.
func (f *fakeSvc12) own(a service.Actor, id int64) (*model.Todo, error) {
	t := f.find(id)
	if t == nil || t.Status == model.TodoDeleted {
		return nil, service.ErrNotFound
	}
	if t.ChatID != a.ChatID {
		return nil, service.ErrForeignChat
	}
	return t, nil
}

func (f *fakeSvc12) Complete(_ context.Context, a service.Actor, id int64) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("Complete")
	t, err := f.own(a, id)
	if err != nil {
		return model.Todo{}, err
	}
	t.Status = model.TodoDone
	return *t, nil
}

func (f *fakeSvc12) Delete(_ context.Context, a service.Actor, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("Delete")
	t, err := f.own(a, id)
	if err != nil {
		return err
	}
	t.Status = model.TodoDeleted
	return nil
}

func (f *fakeSvc12) Edit(_ context.Context, a service.Actor, id int64, text string) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("Edit")
	if f.emptyText != "" && text == f.emptyText {
		return model.Todo{}, service.ErrEmptyText
	}
	t, err := f.own(a, id)
	if err != nil {
		return model.Todo{}, err
	}
	t.Text = text
	return *t, nil
}

func (f *fakeSvc12) Remind(_ context.Context, a service.Actor, id int64, at time.Time) (model.Reminder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("Remind")
	if _, err := f.own(a, id); err != nil {
		return model.Reminder{}, err
	}
	if !at.After(f.clk.Now()) {
		return model.Reminder{}, service.ErrInPast
	}
	f.remindAt = append(f.remindAt, at)
	f.nextID++
	r := model.Reminder{ID: f.nextID, TodoID: id, ChatID: a.ChatID, FireAt: at, Kind: model.ReminderCustom, State: model.ReminderPending}
	f.reminders[r.ID] = &r
	return r, nil
}

func (f *fakeSvc12) Snooze(_ context.Context, a service.Actor, id int64, arg string) (model.Reminder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("Snooze")
	r, ok := f.reminders[id]
	if !ok {
		return model.Reminder{}, service.ErrNotFound
	}
	if r.ChatID != a.ChatID {
		return model.Reminder{}, service.ErrForeignChat
	}
	d := map[string]time.Duration{ui.Snooze10m: 10 * time.Minute, ui.Snooze1h: time.Hour, ui.SnoozeTomorrow: 24 * time.Hour}[arg]
	return model.Reminder{ID: id + 1000, TodoID: r.TodoID, ChatID: r.ChatID, FireAt: f.clk.Now().Add(d), Kind: model.ReminderSnooze, State: model.ReminderPending}, nil
}

func (f *fakeSvc12) AckReminder(_ context.Context, a service.Actor, id int64) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("AckReminder")
	r, ok := f.reminders[id]
	if !ok {
		return model.Todo{}, service.ErrNotFound
	}
	if r.ChatID != a.ChatID {
		return model.Todo{}, service.ErrForeignChat
	}
	t := f.find(r.TodoID)
	t.Status = model.TodoDone
	return *t, nil
}

func (f *fakeSvc12) SetTZ(_ context.Context, userID int64, tz string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetTZ")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return service.ErrBadTZ
	}
	p := f.prefs[userID]
	p.TZ = loc
	f.prefs[userID] = p
	return nil
}

func (f *fakeSvc12) SetDigest(_ context.Context, a service.Actor, hhmm string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec("SetDigest")
	f.digests[a.ChatID] = hhmm
	return nil
}

func (f *fakeSvc12) DigestView(context.Context, int64, time.Time) (model.DigestView, error) {
	return model.DigestView{}, nil
}

func (f *fakeSvc12) MigrateChat(_ context.Context, oldID, newID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.migrated = append(f.migrated, [2]int64{oldID, newID})
	return nil
}

func (f *fakeSvc12) PutPrompt(_ context.Context, p model.Prompt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p.ExpiresAt = f.clk.Now().Add(f.promptTTL)
	f.prompts[[2]int64{p.ChatID, int64(p.MsgID)}] = p
	return nil
}

func (f *fakeSvc12) TakePrompt(_ context.Context, chatID int64, msgID int, userID int64) (model.Prompt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.takeErr != nil {
		return model.Prompt{}, f.takeErr
	}
	key := [2]int64{chatID, int64(msgID)}
	p, ok := f.prompts[key]
	if !ok || p.UserID != userID || !f.clk.Now().Before(p.ExpiresAt) {
		return model.Prompt{}, service.ErrNotFound
	}
	delete(f.prompts, key)
	return p, nil
}

func (f *fakeSvc12) MarkUpdate(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[id] {
		return false, nil
	}
	f.seen[id] = true
	return true, nil
}

// Assignment (#58): not used by the #12 tests.

func (f *fakeSvc12) NoteUsername(context.Context, int64, string) error { return nil }

func (f *fakeSvc12) UserByUsername(context.Context, string) (model.User, error) {
	return model.User{}, service.ErrNotFound
}

func (f *fakeSvc12) AddTodoFor(ctx context.Context, a service.Actor, _ int64, r nlparse.Result) (model.Todo, error) {
	return f.AddTodo(ctx, a, r)
}

func (f *fakeSvc12) ListAssigned(context.Context, int64) ([]model.Todo, error) { return nil, nil }

// Wizard drafts (#68): the fake has none.
func (f *fakeSvc12) StartDraft(context.Context, *model.Draft) error { return nil }

func (f *fakeSvc12) Draft(context.Context, int64, int64) (model.Draft, error) {
	return model.Draft{}, service.ErrNotFound
}

func (f *fakeSvc12) DraftByMessage(context.Context, int64, int) (model.Draft, error) {
	return model.Draft{}, service.ErrNotFound
}

func (f *fakeSvc12) UpdateDraft(context.Context, *model.Draft) error { return service.ErrStaleDraft }

func (f *fakeSvc12) EndDraft(context.Context, model.Draft) error { return service.ErrStaleDraft }
func (f *fakeSvc12) SaveDraft(context.Context, model.Draft, service.Actor, int64, nlparse.Result) (model.Todo, error) {
	return model.Todo{}, service.ErrStaleDraft
}

func (f *fakeSvc12) NamedUsers(context.Context, int64) ([]model.User, error) { return nil, nil }
func (f *fakeSvc12) SetKBVersion(context.Context, int64, int) error          { return nil }

// harness12 wires a Handler to a fakeSvc12 and a FakeSender.
type harness12 struct {
	t   *testing.T
	clk *clocktest.Fake
	svc *fakeSvc12
	fs  *tgtest.FakeSender
	h   *Handler
}

func newHarness12(t *testing.T) *harness12 {
	t.Helper()
	clk := clocktest.New(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	hh := &harness12{t: t, clk: clk, svc: newFakeSvc12(clk), fs: &tgtest.FakeSender{}}
	hh.h = hh.newHandler()
	return hh
}

// newHandler builds a fresh Handler over the same service state (a restart).
func (hh *harness12) newHandler() *Handler {
	return New(Deps{
		Svc:         hh.svc,
		Send:        hh.fs,
		Parser:      nlparse.Parser{DefaultHour: 9},
		Clock:       hh.clk,
		Allowed:     map[int64]bool{1: true, 2: true},
		BotUsername: "TodoBot",
	})
}

func (hh *harness12) press(user, chat int64, msgID int, data string) {
	hh.t.Helper()
	err := hh.h.Handle(context.Background(), tg.Update{Callback: &tg.Callback{ID: "cb", FromID: user, ChatID: chat, MessageID: msgID, Data: data}})
	if err != nil {
		hh.t.Fatal(err)
	}
}

func (hh *harness12) say(user, chat int64, kind model.ChatKind, text string, replyTo int) {
	hh.t.Helper()
	err := hh.h.Handle(context.Background(), tg.Update{Message: &tg.Message{ID: 500, ChatID: chat, ChatKind: kind, FromID: user, Text: text, ReplyToID: replyTo}})
	if err != nil {
		hh.t.Fatal(err)
	}
}

// private sends text from user 1 in their private chat (chat 1).
func (hh *harness12) private(text string) {
	hh.t.Helper()
	hh.say(1, 1, model.ChatPrivate, text, 0)
}

func (hh *harness12) last() tgtest.Sent {
	hh.t.Helper()
	s, ok := hh.fs.Last()
	if !ok {
		hh.t.Fatal("nothing was sent")
	}
	return s
}

func (f *fakeSvc12) UpdateTodo(context.Context, service.Actor, int64, service.TodoPatch) (model.Todo, error) {
	return model.Todo{}, service.ErrNotFound
}

func (f *fakeSvc12) SaveEditDraft(context.Context, model.Draft, service.Actor, int64, service.TodoPatch) (model.Todo, error) {
	return model.Todo{}, service.ErrStaleDraft
}
