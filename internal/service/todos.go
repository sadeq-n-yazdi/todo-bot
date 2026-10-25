package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// completeAheadWindow is how far ahead of its due time a recurring item may
// be completed from the list and still count as this occurrence.
const completeAheadWindow = 12 * time.Hour

// ownedTodo loads a todo and checks that the actor may act on it: it lives in
// the actor's chat, or the actor is its assignee acting from their own
// private chat (ARCHITECTURE §11). The private-chat condition stops a
// modified client from acting on the item from an unrelated group. Deleted
// todos count as missing.
func ownedTodo(ctx context.Context, st store.Store, a Actor, todoID int64) (model.Todo, error) {
	t, err := st.GetTodo(ctx, todoID)
	if err != nil {
		return model.Todo{}, err
	}
	if t.Status == model.TodoDeleted {
		return model.Todo{}, ErrNotFound
	}
	if t.ChatID != a.ChatID && (t.AssigneeID == 0 || t.AssigneeID != a.UserID || a.ChatID != a.UserID) {
		return model.Todo{}, ErrForeignChat
	}
	return t, nil
}

func newReminder(t model.Todo, at time.Time, kind model.ReminderKind, now time.Time) *model.Reminder {
	return &model.Reminder{
		TodoID: t.ID, ChatID: t.ChatID, FireAt: at.UTC(),
		Kind: kind, State: model.ReminderPending, CreatedAt: now,
	}
}

// advanceRecurring moves a recurring todo to its first occurrence after
// `from` (in the todo's own zone) and replaces its pending 'due' reminder.
// It reports false, changing nothing, when the todo has no usable rule.
func (s *Svc) advanceRecurring(ctx context.Context, tx store.Store, t *model.Todo, from, now time.Time) (bool, error) {
	return s.advanceWith(ctx, tx, t, from, now, func(t model.Todo) error { return tx.UpdateTodo(ctx, t) })
}

// advanceWith is advanceRecurring with write as the write of the todo row.
func (s *Svc) advanceWith(ctx context.Context, tx store.Store, t *model.Todo, from, now time.Time, write func(model.Todo) error) (bool, error) {
	if t.Recur == "" {
		return false, nil
	}
	rule, err := recur.Parse(t.Recur)
	if err != nil {
		s.log.Warn("service: todo has an unusable rule", "todo", t.ID, "rule", t.Recur, "err", err)
		return false, nil
	}
	next := rule.Next(from, s.locOrDefault(t.TZ))
	if next.IsZero() {
		return false, nil
	}
	next = next.UTC()
	t.DueAt = &next
	t.UpdatedAt = now
	if err := write(*t); err != nil {
		return false, err
	}
	if err := tx.CancelPending(ctx, t.ID, model.ReminderDue); err != nil {
		return false, err
	}
	if err := tx.InsertReminder(ctx, newReminder(*t, next, model.ReminderDue, now)); err != nil {
		return false, err
	}
	return true, nil
}

// dateOnlySentinel reports whether the parser marked r as "this day, no time
// of day": for today with the default slot already passed it returns
// 23:59 and HasTime=false (see package nlparse). Such an item is overdue
// from the start and gets no reminder.
func dateOnlySentinel(r nlparse.Result, loc *time.Location, now time.Time) bool {
	if r.HasTime || r.Due == nil {
		return false
	}
	d := r.Due.In(loc)
	ny, nm, nd := now.In(loc).Date()
	dy, dm, dd := d.Date()
	return d.Hour() == 23 && d.Minute() == 59 && ny == dy && nm == dm && nd == dd
}

// AddTodo implements Service as AddTodoFor with no assignee.
func (s *Svc) AddTodo(ctx context.Context, a Actor, r nlparse.Result) (model.Todo, error) {
	return s.AddTodoFor(ctx, a, 0, r)
}

// AddTodoFor implements Service. A future due time creates a 'due' reminder. A
// due time that is already past (a date with no time earlier than the default
// slot, or an explicit past time) is stored as overdue without a reminder.
func (s *Svc) AddTodoFor(ctx context.Context, a Actor, assigneeID int64, r nlparse.Result) (model.Todo, error) {
	now := s.clk.Now().UTC()
	var out model.Todo
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		var err error
		out, err = s.addTodoTx(ctx, tx, a, assigneeID, r, now)
		return err
	})
	if err != nil {
		return model.Todo{}, err
	}
	if out.DueAt != nil {
		s.wake()
	}
	return out, nil
}

// addTodoTx is the body of AddTodoFor, inside the caller's transaction tx.
func (s *Svc) addTodoTx(ctx context.Context, tx store.Store, a Actor, assigneeID int64, r nlparse.Result, now time.Time) (model.Todo, error) {
	u, err := s.ensureUser(ctx, tx, a.UserID, "")
	if err != nil {
		return model.Todo{}, err
	}
	if err := s.ensureChat(ctx, tx, a, now); err != nil {
		return model.Todo{}, err
	}
	if strings.TrimSpace(r.Text) == "" {
		return model.Todo{}, ErrEmptyText
	}
	var assigneeName string
	if assigneeID != 0 && assigneeID != a.ChatID {
		as, err := tx.GetUser(ctx, assigneeID)
		if err != nil {
			return model.Todo{}, err
		}
		assigneeName = as.Username
	} else {
		assigneeID = 0 // none, or the assignee's own private chat
	}
	t := model.Todo{
		AssigneeID: assigneeID, AssigneeName: assigneeName,
		ChatID: a.ChatID, Text: strings.TrimSpace(r.Text), Status: model.TodoOpen,
		TZ: u.TZ, CreatedBy: a.UserID, CreatedAt: now, UpdatedAt: now,
	}
	var remindAt *time.Time
	t.DueAt, t.Recur, remindAt, err = scheduleOf(r, s.locOrDefault(u.TZ), now)
	if err != nil {
		return model.Todo{}, err
	}
	if err := tx.InsertTodo(ctx, &t); err != nil {
		return model.Todo{}, err
	}
	if remindAt != nil {
		if err := tx.InsertReminder(ctx, newReminder(t, *remindAt, model.ReminderDue, now)); err != nil {
			return model.Todo{}, err
		}
	}
	return t, nil
}

// scheduleOf turns a parsed When into the schedule of an item, for an
// actor in zone loc at now. Add and edit both use it, so they always agree.
//
// due is the item's due time in UTC, or nil. rule is the rule text, or "".
// A rule whose parsed due time is missing or not in the future starts at
// its next occurrence after now. remindAt is the time of the 'due'
// reminder, or nil when there must be none: no due time, a due time that
// is not in the future, or the parser's date-only sentinel.
func scheduleOf(r nlparse.Result, loc *time.Location, now time.Time) (due *time.Time, rule string, remindAt *time.Time, err error) {
	if r.Due != nil {
		d := r.Due.UTC()
		due = &d
	}
	if r.Rule != nil {
		rule = r.Rule.String()
		if due == nil || !due.After(now) {
			next := r.Rule.Next(now, loc)
			if next.IsZero() {
				return nil, "", nil, fmt.Errorf("service: recurrence %q has no next occurrence", rule)
			}
			next = next.UTC()
			due = &next
		}
	}
	if due != nil && due.After(now) && (r.Rule != nil || !dateOnlySentinel(r, loc, now)) {
		remindAt = due
	}
	return due, rule, remindAt, nil
}

// List implements Service: open todos, ui.PageSize per page. An out-of-range
// page is clamped to the nearest valid one.
func (s *Svc) List(ctx context.Context, chatID int64, page int) ([]model.Todo, int, error) {
	q := model.TodoQuery{ChatID: chatID, Statuses: []model.TodoStatus{model.TodoOpen}}
	n, err := s.st.CountTodos(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	pages := max(1, (n+ui.PageSize-1)/ui.PageSize)
	page = min(max(page, 0), pages-1)
	q.Limit, q.Offset = ui.PageSize, page*ui.PageSize
	todos, err := s.st.ListTodos(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	return todos, pages, nil
}

// ByNum implements Service. Deleted todos count as missing.
func (s *Svc) ByNum(ctx context.Context, chatID int64, num int) (model.Todo, error) {
	t, err := s.st.GetTodoByNum(ctx, chatID, num)
	if err != nil {
		return model.Todo{}, err
	}
	if t.Status == model.TodoDeleted {
		return model.Todo{}, ErrNotFound
	}
	return t, nil
}

// Todo returns one todo of the actor's chat, done items included. It is
// ErrNotFound if the todo is missing or deleted and ErrForeignChat if it
// belongs to another chat.
func (s *Svc) Todo(ctx context.Context, a Actor, id int64) (model.Todo, error) {
	return ownedTodo(ctx, s.st, a, id)
}

// Complete implements Service. A one-off item becomes done and loses its
// pending reminders. A recurring item advances to its next occurrence only
// if it is due within 12 hours; otherwise it is returned unchanged so the
// caller can show the next due date.
//
// changed is true only if this call changed the item. The write is
// conditional (store.UpdateOpenTodo): of several concurrent calls on one
// open item, also on several nodes, exactly one changes it, so the caller
// sends the "did" notice one time (#147).
func (s *Svc) Complete(ctx context.Context, a Actor, todoID int64) (model.Todo, bool, error) {
	now := s.clk.Now().UTC()
	var out model.Todo
	changed := false
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		t, err := ownedTodo(ctx, tx, a, todoID)
		if err != nil {
			return err
		}
		out = t
		if t.Status != model.TodoOpen {
			return nil
		}
		write := openWrite(ctx, tx, t.DueAt)
		if t.Recur != "" {
			if t.DueAt != nil && t.DueAt.After(now.Add(completeAheadWindow)) {
				return nil
			}
			from := now
			if t.DueAt != nil && t.DueAt.After(from) {
				from = *t.DueAt
			}
			ok, err := s.advanceWith(ctx, tx, &t, from, now, write)
			if err != nil {
				return err
			}
			if ok {
				if err := tx.CancelPending(ctx, t.ID, model.ReminderSnooze); err != nil {
					return err
				}
				out, changed = t, true
				return nil
			}
		}
		out, err = markDoneWith(ctx, tx, t, now, write)
		changed = err == nil
		return err
	})
	if errors.Is(err, errNotChanged) {
		// Another call changed the row first; nothing was written here.
		out, err = ownedTodo(ctx, s.st, a, todoID)
		return out, false, err
	}
	if err != nil {
		return model.Todo{}, false, err
	}
	if changed {
		s.wake()
	}
	return out, changed, nil
}

// errNotChanged rolls back a Complete or AckReminder whose conditional
// write lost to another call.
var errNotChanged = errors.New("service: todo changed meanwhile")

// openWrite returns the conditional write of Complete and AckReminder: it
// writes the todo only if the row is still as read, that is open and, if
// it repeats, with the due date read. Otherwise it returns errNotChanged.
func openWrite(ctx context.Context, tx store.Store, read *time.Time) func(model.Todo) error {
	return func(t model.Todo) error {
		ok, err := tx.UpdateOpenTodo(ctx, t, read)
		if err == nil && !ok {
			err = errNotChanged
		}
		return err
	}
}

// markDoneWith marks t done, with write as the write of the todo row, and
// cancels its pending reminders.
func markDoneWith(ctx context.Context, tx store.Store, t model.Todo, now time.Time, write func(model.Todo) error) (model.Todo, error) {
	t.Status, t.DoneAt, t.UpdatedAt = model.TodoDone, &now, now
	if err := write(t); err != nil {
		return model.Todo{}, err
	}
	if err := tx.CancelPending(ctx, t.ID); err != nil {
		return model.Todo{}, err
	}
	return t, nil
}

// Delete implements Service; it also stops a recurring item.
func (s *Svc) Delete(ctx context.Context, a Actor, todoID int64) error {
	now := s.clk.Now().UTC()
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		t, err := ownedTodo(ctx, tx, a, todoID)
		if err != nil {
			return err
		}
		t.Status, t.UpdatedAt = model.TodoDeleted, now
		if err := tx.UpdateTodo(ctx, t); err != nil {
			return err
		}
		return tx.CancelPending(ctx, t.ID)
	})
	if err != nil {
		return err
	}
	s.wake()
	return nil
}

// Edit implements Service. The read and the write share one transaction so a
// concurrent Complete, Delete, SetTZ or recurrence advance is not undone.
func (s *Svc) Edit(ctx context.Context, a Actor, todoID int64, text string) (model.Todo, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return model.Todo{}, ErrEmptyText
	}
	var out model.Todo
	err := s.st.WithTx(ctx, func(tx store.Store) error {
		t, err := ownedTodo(ctx, tx, a, todoID)
		if err != nil {
			return err
		}
		t.Text, t.UpdatedAt = text, s.clk.Now().UTC()
		out = t
		return tx.UpdateTodo(ctx, t)
	})
	if err != nil {
		return model.Todo{}, err
	}
	return out, nil
}

// Remind implements Service.
func (s *Svc) Remind(ctx context.Context, a Actor, todoID int64, at time.Time) (model.Reminder, error) {
	now := s.clk.Now().UTC()
	if !at.After(now) {
		return model.Reminder{}, ErrInPast
	}
	t, err := ownedTodo(ctx, s.st, a, todoID)
	if err != nil {
		return model.Reminder{}, err
	}
	if t.Status != model.TodoOpen {
		return model.Reminder{}, ErrNotFound
	}
	r := newReminder(t, at, model.ReminderCustom, now)
	if err := s.st.InsertReminder(ctx, r); err != nil {
		return model.Reminder{}, err
	}
	s.wake()
	return *r, nil
}

// reminderOf loads a reminder and its open todo, checking the chat.
func (s *Svc) reminderOf(ctx context.Context, a Actor, reminderID int64) (model.Reminder, model.Todo, error) {
	r, err := s.st.GetReminder(ctx, reminderID)
	if err != nil {
		return model.Reminder{}, model.Todo{}, err
	}
	// The check is on the todo, not on r.ChatID: a reminder delivered by DM
	// keeps the creating chat in r.ChatID but is pressed from the assignee's.
	t, err := ownedTodo(ctx, s.st, a, r.TodoID)
	if err != nil {
		return model.Reminder{}, model.Todo{}, err
	}
	return r, t, nil
}

// Snooze implements Service: arg is ui.Snooze10m, ui.Snooze1h or
// ui.SnoozeTomorrow. "Tomorrow" is the next local calendar day at the
// configured default time, in the actor's zone.
func (s *Svc) Snooze(ctx context.Context, a Actor, reminderID int64, arg string) (model.Reminder, error) {
	now := s.clk.Now().UTC()
	var at time.Time
	switch arg {
	case ui.Snooze10m:
		at = now.Add(10 * time.Minute)
	case ui.Snooze1h:
		at = now.Add(time.Hour)
	case ui.SnoozeTomorrow:
		p, err := s.Prefs(ctx, a.UserID, "")
		if err != nil {
			return model.Reminder{}, err
		}
		y, m, d := now.In(p.TZ).Date()
		at = time.Date(y, m, d+1, s.cfg.DefaultHour, s.cfg.DefaultMinute, 0, 0, p.TZ)
	default:
		return model.Reminder{}, fmt.Errorf("service: unknown snooze %q", arg)
	}
	_, t, err := s.reminderOf(ctx, a, reminderID)
	if err != nil {
		return model.Reminder{}, err
	}
	if t.Status != model.TodoOpen {
		return model.Reminder{}, ErrNotFound
	}
	r := newReminder(t, at, model.ReminderSnooze, now)
	if err := s.st.InsertReminder(ctx, r); err != nil {
		return model.Reminder{}, err
	}
	s.wake()
	return *r, nil
}

// AckReminder implements Service: Done on a reminder message. A one-off item
// is completed. A recurring item is left alone, because the scheduler
// already advanced it when the reminder fired; advancing here too would skip
// the next occurrence. Only leftover snoozes of the occurrence are dropped.
//
// changed is true only if this call changed something, so the caller sends
// the "did" notice one time, also when several presses race on several
// nodes (#157). A one-off item is written with store.UpdateOpenTodo, as in
// Complete. For a recurring item the winner is the first call that marks
// the sent reminder acked (store.MarkReminderAcked); only the winner drops
// the snoozes. A press on a reminder that is not marked sent yet changes
// nothing on a recurring item.
func (s *Svc) AckReminder(ctx context.Context, a Actor, reminderID int64) (model.Todo, bool, error) {
	now := s.clk.Now().UTC()
	r, err := s.st.GetReminder(ctx, reminderID)
	if err != nil {
		return model.Todo{}, false, err
	}
	var out model.Todo
	changed := false
	err = s.st.WithTx(ctx, func(tx store.Store) error {
		t, err := ownedTodo(ctx, tx, a, r.TodoID)
		if err != nil {
			return err
		}
		out = t
		if t.Status != model.TodoOpen {
			return nil
		}
		if t.Recur != "" {
			ok, err := tx.MarkReminderAcked(ctx, r.ID, now)
			if err != nil || !ok {
				return err
			}
			changed = true
			return tx.CancelPending(ctx, t.ID, model.ReminderSnooze)
		}
		out, err = markDoneWith(ctx, tx, t, now, openWrite(ctx, tx, t.DueAt))
		changed = err == nil
		return err
	})
	if errors.Is(err, errNotChanged) {
		// Another call changed the row first; nothing was written here.
		out, err = ownedTodo(ctx, s.st, a, r.TodoID)
		return out, false, err
	}
	if err != nil {
		return model.Todo{}, false, err
	}
	if changed {
		s.wake()
	}
	return out, changed, nil
}
