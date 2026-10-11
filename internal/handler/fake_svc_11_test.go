package handler

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// fakeSvc11 is an in-memory service.Service for the #11 tests. Methods the
// todo commands never call panic through the nil embedded interface.
type fakeSvc11 struct {
	service.Service
	mu     sync.Mutex
	prefs  map[int64]model.Prefs
	todos  []*model.Todo
	added  []nlparse.Result // every Result passed to AddTodo
	addErr error            // returned by AddTodo when set
	nextID int64
	clock  func() time.Time
	// failWith, if set, is returned by Complete, Delete and Edit.
	failWith error
}

func newFakeSvc11(clock func() time.Time) *fakeSvc11 {
	return &fakeSvc11{prefs: map[int64]model.Prefs{}, clock: clock}
}

func (f *fakeSvc11) Prefs(_ context.Context, userID int64, _ string) (model.Prefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prefs[userID], nil
}

func (f *fakeSvc11) AddTodo(_ context.Context, a service.Actor, r nlparse.Result) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, r)
	if f.addErr != nil {
		return model.Todo{}, f.addErr
	}
	f.nextID++
	num := 1
	for _, t := range f.todos {
		if t.ChatID == a.ChatID && t.Num >= num {
			num = t.Num + 1
		}
	}
	t := &model.Todo{ID: f.nextID, ChatID: a.ChatID, Num: num, Text: r.Text, Status: model.TodoOpen,
		DueAt: r.Due, CreatedBy: a.UserID, CreatedAt: f.clock()}
	if r.Rule != nil {
		t.Recur = r.Rule.String()
	}
	f.todos = append(f.todos, t)
	return *t, nil
}

func (f *fakeSvc11) open(chatID int64) []model.Todo {
	var out []model.Todo
	for _, t := range f.todos {
		if t.ChatID == chatID && t.Status == model.TodoOpen {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

func (f *fakeSvc11) List(_ context.Context, chatID int64, page int) ([]model.Todo, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.open(chatID)
	pages := max(1, (len(all)+ui.PageSize-1)/ui.PageSize)
	lo := min(page*ui.PageSize, len(all))
	hi := min(lo+ui.PageSize, len(all))
	return all[lo:hi], pages, nil
}

func (f *fakeSvc11) ByNum(_ context.Context, chatID int64, num int) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.open(chatID) {
		if t.Num == num {
			return t, nil
		}
	}
	return model.Todo{}, service.ErrNotFound
}

func (f *fakeSvc11) byID(id int64) *model.Todo {
	for _, t := range f.todos {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (f *fakeSvc11) Complete(_ context.Context, a service.Actor, id int64) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.byID(id)
	switch {
	case f.failWith != nil:
		return model.Todo{}, f.failWith
	case t == nil:
		return model.Todo{}, service.ErrNotFound
	case t.ChatID != a.ChatID:
		return model.Todo{}, service.ErrForeignChat
	}
	t.Status = model.TodoDone
	return *t, nil
}

func (f *fakeSvc11) Delete(_ context.Context, a service.Actor, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.byID(id)
	switch {
	case f.failWith != nil:
		return f.failWith
	case t == nil:
		return service.ErrNotFound
	case t.ChatID != a.ChatID:
		return service.ErrForeignChat
	}
	t.Status = model.TodoDeleted
	return nil
}

func (f *fakeSvc11) Edit(_ context.Context, a service.Actor, id int64, text string) (model.Todo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.byID(id)
	switch {
	case f.failWith != nil:
		return model.Todo{}, f.failWith
	case t == nil:
		return model.Todo{}, service.ErrNotFound
	case t.ChatID != a.ChatID:
		return model.Todo{}, service.ErrForeignChat
	}
	t.Text = text
	return *t, nil
}

func (f *fakeSvc11) DigestView(_ context.Context, chatID int64, now time.Time) (model.DigestView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := model.DigestView{ChatID: chatID, Loc: time.UTC}
	for _, t := range f.open(chatID) {
		switch {
		case t.DueAt == nil:
			v.NoDateCount++
		case t.DueAt.Before(now):
			v.Overdue = append(v.Overdue, t)
		default:
			v.Today = append(v.Today, t)
		}
	}
	return v, nil
}

func (f *fakeSvc11) MarkUpdate(context.Context, int64) (bool, error) { return true, nil }

// NoteUsername, UserByUsername and ListAssigned keep newReq (which records
// the sender's username on every update) and /list off the nil embedded
// interface. The assignment tests use fakeAssign instead.
func (f *fakeSvc11) NoteUsername(context.Context, int64, string) error { return nil }

func (f *fakeSvc11) UserByUsername(context.Context, string) (model.User, error) {
	return model.User{}, service.ErrNotFound
}

func (f *fakeSvc11) ListAssigned(context.Context, int64) ([]model.Todo, error) { return nil, nil }

// Wizard drafts (#68): this fake has none. SetKBVersion is kept in prefs so
// the reply keyboard is attached once.
func (f *fakeSvc11) StartDraft(context.Context, *model.Draft) error { return nil }

func (f *fakeSvc11) Draft(context.Context, int64, int64) (model.Draft, error) {
	return model.Draft{}, service.ErrNotFound
}

func (f *fakeSvc11) DraftByMessage(context.Context, int64, int) (model.Draft, error) {
	return model.Draft{}, service.ErrNotFound
}

func (f *fakeSvc11) UpdateDraft(context.Context, *model.Draft) error { return service.ErrStaleDraft }
func (f *fakeSvc11) EndDraft(context.Context, model.Draft) error     { return service.ErrStaleDraft }

func (f *fakeSvc11) SaveDraft(context.Context, model.Draft, service.Actor, int64, nlparse.Result) (model.Todo, error) {
	return model.Todo{}, service.ErrStaleDraft
}

func (f *fakeSvc11) NamedUsers(context.Context, int64) ([]model.User, error) { return nil, nil }

func (f *fakeSvc11) SetKBVersion(_ context.Context, id int64, v int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.prefs[id]
	p.KBVersion = v
	f.prefs[id] = p
	return nil
}
