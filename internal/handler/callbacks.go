package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Callback data for the /lang and /calendar buttons. ui.Encode only knows
// the todo/reminder actions, so settings buttons use their own short
// prefixes ("sl:fa", "sc:jalali"), which cannot clash with a ui.ActionKind.
const (
	settingLang = "sl"
	settingCal  = "sc"
)

func settingData(kind, val string) string { return kind + ":" + val }

func parseSetting(data string) (kind, val string, ok bool) {
	kind, val, ok = strings.Cut(data, ":")
	return kind, val, ok && (kind == settingLang || kind == settingCal)
}

// onCallback handles an inline button press from an allowlisted user. It
// always answers the callback exactly once (or returns an error before
// answering, so Handle can answer with a generic message), and rejects
// actions on todos/reminders whose ChatID differs from cb.ChatID.
func (h *Handler) onCallback(ctx context.Context, cb *tg.Callback, r req) error {
	var (
		note string
		err  error
	)
	if kind, val, ok := parseSetting(cb.Data); ok {
		note, err = h.settingCallback(ctx, cb, r, kind, val)
	} else if act, derr := ui.Decode(cb.Data); derr == nil {
		note, err = h.actionCallback(ctx, cb, r, act)
	} else {
		h.Log.Warn("bad callback data", "data", cb.Data)
	}
	if err != nil {
		return err
	}
	h.logErr("answer callback", h.Send.AnswerCallback(ctx, cb.ID, note))
	return nil
}

func (h *Handler) tz(r req) *time.Location {
	if r.Prefs.TZ == nil {
		return time.UTC
	}
	return r.Prefs.TZ
}

// isGone reports whether err means the item is missing or not in this chat.
func isGone(err error) bool {
	return errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrForeignChat)
}

// ownedTodo finds the open todo id in chatID. A todo of another chat is
// indistinguishable from a missing one, so ids cannot be probed.
func (h *Handler) ownedTodo(ctx context.Context, chatID, id int64) (model.Todo, error) {
	for page := 0; ; page++ {
		ts, pages, err := h.Svc.List(ctx, chatID, page)
		if err != nil {
			return model.Todo{}, err
		}
		for _, td := range ts {
			if td.ID == id {
				return td, nil
			}
		}
		if page+1 >= pages {
			return model.Todo{}, service.ErrNotFound
		}
	}
}

// listView renders list page `page` (clamped to the last page), optionally
// under a status line.
func (h *Handler) listView(ctx context.Context, chatID int64, r req, page int, status string) (tg.OutMessage, error) {
	ts, pages, err := h.Svc.List(ctx, chatID, page)
	if err != nil {
		return tg.OutMessage{}, err
	}
	if len(ts) == 0 && page > 0 {
		page = max(pages-1, 0)
		if ts, pages, err = h.Svc.List(ctx, chatID, page); err != nil {
			return tg.OutMessage{}, err
		}
	}
	out := ui.TodoList(ts, r.Prefs.Locale, h.tz(r), h.Clock.Now(), page, pages)
	if status != "" {
		out.Text = status + "\n\n" + out.Text
	}
	return out, nil
}

func (h *Handler) editCallbackMsg(ctx context.Context, cb *tg.Callback, out tg.OutMessage) {
	h.logErr("edit message", h.Send.Edit(ctx, cb.ChatID, cb.MessageID, out))
}

// actionCallback runs a ui action. It returns the text for AnswerCallback.
func (h *Handler) actionCallback(ctx context.Context, cb *tg.Callback, r req, act ui.Action) (string, error) {
	lc := r.Prefs.Locale
	gone := lc.T("settings.cb.gone")
	switch act.Kind {
	case ui.ActPage:
		page, err := strconv.Atoi(act.Arg)
		if err != nil || page < 0 {
			return "", nil
		}
		return "", h.showList(ctx, cb, r, page, "")
	case ui.ActCancel:
		return "", h.showList(ctx, cb, r, 0, "")
	case ui.ActReminderDone:
		td, err := h.Svc.AckReminder(ctx, r.Actor, act.ID)
		if isGone(err) {
			return gone, nil
		} else if err != nil {
			return "", err
		}
		h.editCallbackMsg(ctx, cb, tg.OutMessage{Text: "✅ " + ui.Escape(td.Text)})
		return "", nil
	case ui.ActSnooze:
		rem, err := h.Svc.Snooze(ctx, r.Actor, act.ID, act.Arg)
		if isGone(err) {
			return gone, nil
		} else if err != nil {
			return "", err
		}
		when := ui.FormatWhen(rem.FireAt, lc, h.tz(r), h.Clock.Now())
		h.editCallbackMsg(ctx, cb, tg.OutMessage{Text: lc.T("settings.cb.snoozed", when)})
		return "", nil
	}

	// The remaining actions all target a todo of this chat.
	td, err := h.ownedTodo(ctx, cb.ChatID, act.ID)
	if isGone(err) {
		return gone, nil
	} else if err != nil {
		return "", err
	}
	switch act.Kind {
	case ui.ActDone:
		done, err := h.Svc.Complete(ctx, r.Actor, td.ID)
		if isGone(err) {
			return gone, nil
		} else if err != nil {
			return "", err
		}
		return "", h.showList(ctx, cb, r, 0, lc.T("settings.cb.done", ui.Escape(done.Text)))
	case ui.ActDelete:
		h.editCallbackMsg(ctx, cb, tg.OutMessage{
			Text: lc.T("settings.cb.confirm", ui.Escape(td.Text)),
			Keyboard: [][]tg.Button{{
				{Text: lc.T("settings.btn.delete"), Data: ui.Encode(ui.Action{Kind: ui.ActDeleteConfirm, ID: td.ID})},
				{Text: lc.T("settings.btn.cancel"), Data: ui.Encode(ui.Action{Kind: ui.ActCancel})},
			}},
		})
		return "", nil
	case ui.ActDeleteConfirm:
		err := h.Svc.Delete(ctx, r.Actor, td.ID)
		if isGone(err) {
			return gone, nil
		} else if err != nil {
			return "", err
		}
		return "", h.showList(ctx, cb, r, 0, lc.T("settings.cb.deleted", ui.Escape(td.Text)))
	case ui.ActEdit:
		text := lc.T("settings.prompt.edit", lc.Num(td.Num), ui.Escape(td.Text))
		return "", h.sendPrompt(ctx, cb, model.PromptEdit, td.ID, text)
	case ui.ActRemind:
		text := lc.T("settings.prompt.when", lc.Num(td.Num), ui.Escape(td.Text))
		return "", h.sendPrompt(ctx, cb, model.PromptRemind, td.ID, text)
	}
	return "", nil
}

func (h *Handler) showList(ctx context.Context, cb *tg.Callback, r req, page int, status string) error {
	out, err := h.listView(ctx, cb.ChatID, r, page, status)
	if err != nil {
		return err
	}
	h.editCallbackMsg(ctx, cb, out)
	return nil
}

// sendPrompt sends a ForceReply question and persists it, so any node (or a
// restarted process) can complete it when the user replies.
func (h *Handler) sendPrompt(ctx context.Context, cb *tg.Callback, kind model.PromptKind, todoID int64, text string) error {
	id, err := h.Send.Send(ctx, cb.ChatID, tg.OutMessage{Text: text, ForceReply: true})
	if err != nil {
		return err
	}
	return h.Svc.PutPrompt(ctx, model.Prompt{ChatID: cb.ChatID, MsgID: id, UserID: cb.FromID, Kind: kind, TodoID: todoID})
}

// settingCallback handles the /lang and /calendar buttons.
func (h *Handler) settingCallback(ctx context.Context, cb *tg.Callback, r req, kind, val string) (string, error) {
	lc := r.Prefs.Locale
	switch kind {
	case settingLang:
		lang := i18n.Lang(val)
		if !lang.Valid() {
			return "", nil
		}
		if err := h.Svc.SetLang(ctx, cb.FromID, lang); err != nil {
			return "", err
		}
		lc.Lang = lang
		h.editCallbackMsg(ctx, cb, tg.OutMessage{Text: lc.T("settings.lang.set", langName(lc, lang))})
	case settingCal:
		cal := calendar.Kind(val)
		if !cal.Valid() {
			return "", nil
		}
		if err := h.Svc.SetCalendar(ctx, cb.FromID, cal); err != nil {
			return "", err
		}
		lc.Cal = cal
		h.editCallbackMsg(ctx, cb, tg.OutMessage{Text: lc.T("settings.cal.set", calName(lc, cal))})
	}
	return "", nil
}

func langName(lc i18n.Locale, l i18n.Lang) string { return lc.T("settings.lang." + string(l)) }

func calName(lc i18n.Locale, k calendar.Kind) string {
	if k == calendar.Jalali {
		return lc.T("settings.cal.jalali")
	}
	return lc.T("settings.cal.greg")
}
