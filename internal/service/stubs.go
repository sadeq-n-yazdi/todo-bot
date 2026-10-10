package service

import (
	"context"
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
)

// All methods below are TODO(#8).

var errNotImplemented = errors.New("service: not implemented")

func (s *Svc) Prefs(ctx context.Context, userID int64, tgLang string) (model.Prefs, error) {
	return model.Prefs{}, errNotImplemented
}
func (s *Svc) SetLang(ctx context.Context, userID int64, lang i18n.Lang) error {
	return errNotImplemented
}
func (s *Svc) SetCalendar(ctx context.Context, userID int64, cal calendar.Kind) error {
	return errNotImplemented
}
func (s *Svc) AddTodo(ctx context.Context, a Actor, r nlparse.Result) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (s *Svc) List(ctx context.Context, chatID int64, page int) ([]model.Todo, int, error) {
	return nil, 0, errNotImplemented
}
func (s *Svc) ByNum(ctx context.Context, chatID int64, num int) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (s *Svc) Complete(ctx context.Context, a Actor, todoID int64) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (s *Svc) Delete(ctx context.Context, a Actor, todoID int64) error { return errNotImplemented }
func (s *Svc) Edit(ctx context.Context, a Actor, todoID int64, text string) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (s *Svc) Remind(ctx context.Context, a Actor, todoID int64, at time.Time) (model.Reminder, error) {
	return model.Reminder{}, errNotImplemented
}
func (s *Svc) Snooze(ctx context.Context, a Actor, reminderID int64, arg string) (model.Reminder, error) {
	return model.Reminder{}, errNotImplemented
}
func (s *Svc) AckReminder(ctx context.Context, a Actor, reminderID int64) (model.Todo, error) {
	return model.Todo{}, errNotImplemented
}
func (s *Svc) SetTZ(ctx context.Context, userID int64, tz string) error  { return errNotImplemented }
func (s *Svc) SetDigest(ctx context.Context, a Actor, hhmm string) error { return errNotImplemented }
func (s *Svc) DigestView(ctx context.Context, chatID int64, now time.Time) (model.DigestView, error) {
	return model.DigestView{}, errNotImplemented
}
func (s *Svc) MigrateChat(ctx context.Context, oldID, newID int64) error { return errNotImplemented }

// scheduler.Backend

func (s *Svc) NextWakeup(ctx context.Context) (time.Time, bool, error) {
	return time.Time{}, false, errNotImplemented
}
func (s *Svc) ClaimReminders(ctx context.Context, now time.Time, limit int) ([]model.DueReminder, error) {
	return nil, errNotImplemented
}
func (s *Svc) ReminderSent(ctx context.Context, id int64, msgID int, now time.Time) error {
	return errNotImplemented
}
func (s *Svc) ReminderFailed(ctx context.Context, id int64, now time.Time, permanent bool) error {
	return errNotImplemented
}
func (s *Svc) CancelReminder(ctx context.Context, id int64) error { return errNotImplemented }
func (s *Svc) ClaimDigests(ctx context.Context, now time.Time) ([]model.Chat, error) {
	return nil, errNotImplemented
}
func (s *Svc) DigestDone(ctx context.Context, chatID int64, now time.Time) error {
	return errNotImplemented
}
func (s *Svc) Housekeep(ctx context.Context, now time.Time) error { return errNotImplemented }

func (s *Svc) PutPrompt(ctx context.Context, p model.Prompt) error { return errNotImplemented }
func (s *Svc) TakePrompt(ctx context.Context, chatID int64, msgID int) (model.Prompt, error) {
	return model.Prompt{}, errNotImplemented
}
func (s *Svc) MarkUpdate(ctx context.Context, updateID int64) (bool, error) {
	return false, errNotImplemented
}
