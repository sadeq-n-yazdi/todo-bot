package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// ForceReply prompt flows (edit text, set reminder time) and group ->
// supergroup migration.
//
// Pending prompts are persisted via Svc.PutPrompt / Svc.TakePrompt so that
// any node, or a restarted process, can complete them.

// onReply handles a reply to one of our prompts (Svc.TakePrompt on
// m.ChatID, m.ReplyToID, m.FromID). handled=false means the message was not
// a reply to a pending prompt of this user (unknown, expired, taken, or
// someone else's) and should be processed normally.
func (h *Handler) onReply(ctx context.Context, m *tg.Message, r req) (handled bool, err error) {
	p, err := h.Svc.TakePrompt(ctx, m.ChatID, m.ReplyToID, m.FromID)
	if errors.Is(err, service.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		h.Log.Error("take prompt", "chat", m.ChatID, "msg", m.ReplyToID, "err", err)
		return false, nil
	}
	switch p.Kind {
	case model.PromptEdit:
		return true, h.finishEdit(ctx, m, r, p)
	case model.PromptRemind:
		return true, h.finishRemind(ctx, m, r, p)
	}
	return true, nil
}

// retry tells the user what was wrong and re-opens the prompt, so they can
// answer the same message again.
func (h *Handler) retry(ctx context.Context, m *tg.Message, r req, p model.Prompt, key string) error {
	if err := h.Svc.PutPrompt(ctx, p); err != nil {
		return err
	}
	return h.say(ctx, m, r.Prefs.Locale.T(key))
}

func (h *Handler) finishEdit(ctx context.Context, m *tg.Message, r req, p model.Prompt) error {
	lc := r.Prefs.Locale
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return h.retry(ctx, m, r, p, "settings.prompt.none")
	}
	td, err := h.Svc.Edit(ctx, r.Actor, p.TodoID, text)
	if isGone(err) {
		return h.say(ctx, m, lc.T("settings.cb.gone"))
	} else if err != nil {
		return err
	}
	return h.say(ctx, m, lc.T("settings.prompt.done", lc.Num(td.Num), ui.Escape(td.Text)))
}

func (h *Handler) finishRemind(ctx context.Context, m *tg.Message, r req, p model.Prompt) error {
	lc := r.Prefs.Locale
	now := h.Clock.Now().In(h.tz(r))
	at, rule, err := h.Parser.ParseWhen(m.Text, now, lc.Cal)
	switch {
	case errors.Is(err, nlparse.ErrNoDate):
		return h.retry(ctx, m, r, p, "settings.remind.nowhen")
	case err != nil:
		return err
	case rule != nil:
		return h.retry(ctx, m, r, p, "settings.remind.recur")
	}
	rem, err := h.Svc.Remind(ctx, r.Actor, p.TodoID, at)
	switch {
	case errors.Is(err, service.ErrInPast):
		return h.retry(ctx, m, r, p, "settings.remind.past")
	case isGone(err):
		return h.say(ctx, m, lc.T("settings.cb.gone"))
	case err != nil:
		return err
	}
	when := ui.FormatWhen(rem.FireAt, lc, h.tz(r), now)
	return h.say(ctx, m, lc.T("settings.remind.set", when))
}

// onMigrate handles a migrate_to_chat_id service message.
func (h *Handler) onMigrate(ctx context.Context, m *tg.Message) error {
	return h.Svc.MigrateChat(ctx, m.ChatID, m.MigrateToChatID)
}
