package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Owned by #11. Each command replies to the user itself for expected errors
// (bad args, not found) and returns nil; it returns an error only for
// unexpected failures.

// maxTodoChars caps the text of an item. tgbot.Send rejects messages over
// 4096 UTF-16 units and does not split them, so one item must stay small.
const maxTodoChars = 1000

// cmdStart and cmdHelp always carry the "➕ New task" keyboard in a private
// chat, so a user who removed it can get it back.
func (h *Handler) cmdStart(ctx context.Context, m *tg.Message, r req, args string) error {
	return h.sayWithKeyboard(ctx, m, r.Prefs.Locale, r.Prefs.Locale.T("todo.help"))
}

func (h *Handler) cmdHelp(ctx context.Context, m *tg.Message, r req, args string) error {
	return h.sayWithKeyboard(ctx, m, r.Prefs.Locale, r.Prefs.Locale.T("todo.help"))
}

func (h *Handler) cmdAdd(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	input := strings.TrimSpace(args)
	if input == "" {
		// A bare /add starts the step-by-step wizard (#68).
		return h.startWizard(ctx, r, m.ChatID, m.ID)
	}
	if tooLong(input) {
		return h.sayTooLong(ctx, m, lc, input)
	}
	assigneeName, input := splitMention(input, h.BotUsername)
	loc := tzOf(r)
	// Parse normalises digits itself for matching but keeps the user's own
	// characters in Result.Text, so Farsi digits in the text survive.
	res, err := h.Parser.Parse(input, h.Clock.Now().In(loc), lc.Cal)
	if err != nil {
		return err
	}
	if strings.TrimSpace(res.Text) == "" {
		return h.say(ctx, m, lc.T("todo.usage.add"))
	}
	var td model.Todo
	if assigneeName == "" {
		td, err = h.Svc.AddTodo(ctx, r.Actor, res)
	} else {
		assignee, ok, rerr := h.resolveAssignee(ctx, assigneeName)
		if rerr != nil {
			return rerr
		}
		if !ok {
			if h.BotUsername == "" {
				return h.say(ctx, m, lc.T("todo.assign.unknown.nobot", ui.Escape(assigneeName)))
			}
			return h.say(ctx, m, lc.T("todo.assign.unknown", ui.Escape(assigneeName), ui.Escape(h.BotUsername)))
		}
		td, err = h.Svc.AddTodoFor(ctx, r.Actor, assignee.ID, res)
		if err == nil && td.AssigneeID != 0 && td.AssigneeName == "" {
			td.AssigneeName = assignee.Username
		}
	}
	if err != nil {
		return h.todoErr(ctx, m, lc, err)
	}
	return h.say(ctx, m, lc.T("todo.added", lc.Num(td.Num), h.itemSummary(td, r)))
}

func (h *Handler) cmdList(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	page := 1
	if a := normArg(args); a != "" {
		n, err := strconv.Atoi(a)
		if err != nil || n < 1 {
			return h.say(ctx, m, lc.T("todo.usage.list"))
		}
		page = n
	}
	todos, pages, err := h.Svc.List(ctx, r.Actor.ChatID, page-1)
	if err != nil {
		return err
	}
	if page > pages {
		// Past the end: show the last page instead of an empty one.
		page = pages
		if todos, pages, err = h.Svc.List(ctx, r.Actor.ChatID, page-1); err != nil {
			return err
		}
	}
	return h.reply(ctx, m, h.threaded(m, h.renderList(ctx, r, r.Actor.ChatID, todos, page-1, pages)))
}

func (h *Handler) cmdDone(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	td, ok, err := h.todoByArg(ctx, m, r, args, "todo.usage.done")
	if !ok {
		return err
	}
	done, err := h.Svc.Complete(ctx, r.Actor, td.ID)
	if err != nil {
		return h.todoErr(ctx, m, lc, err)
	}
	h.notifyDid(ctx, r, done)
	text := lc.T("todo.done", lc.Num(done.Num), echo(done.Text))
	// A recurring item stays open with its next due time.
	if done.Status == model.TodoOpen && done.DueAt != nil {
		text += "\n" + lc.T("todo.done.next", ui.FormatWhen(*done.DueAt, lc, tzOf(r), h.Clock.Now()))
	}
	return h.say(ctx, m, text)
}

func (h *Handler) cmdDel(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	td, ok, err := h.todoByArg(ctx, m, r, args, "todo.usage.del")
	if !ok {
		return err
	}
	if err := h.Svc.Delete(ctx, r.Actor, td.ID); err != nil {
		return h.todoErr(ctx, m, lc, err)
	}
	return h.say(ctx, m, lc.T("todo.deleted", lc.Num(td.Num), echo(td.Text)))
}

func (h *Handler) cmdEdit(ctx context.Context, m *tg.Message, r req, args string) error {
	lc := r.Prefs.Locale
	args = strings.TrimSpace(args)
	numStr := ""
	if f := strings.Fields(args); len(f) > 0 {
		numStr = f[0]
	}
	text := strings.TrimSpace(strings.TrimPrefix(args, numStr))
	if text == "" {
		return h.say(ctx, m, lc.T("todo.usage.edit"))
	}
	if tooLong(text) {
		return h.sayTooLong(ctx, m, lc, text)
	}
	td, ok, err := h.todoByArg(ctx, m, r, numStr, "todo.usage.edit")
	if !ok {
		return err
	}
	edited, err := h.Svc.Edit(ctx, r.Actor, td.ID, text)
	if err != nil {
		return h.todoErr(ctx, m, lc, err)
	}
	return h.say(ctx, m, lc.T("todo.edited", lc.Num(edited.Num), echo(edited.Text)))
}

func (h *Handler) cmdToday(ctx context.Context, m *tg.Message, r req, args string) error {
	now := h.Clock.Now()
	v, err := h.Svc.DigestView(ctx, r.Actor.ChatID, now)
	if err != nil {
		return err
	}
	// Show the digest in the asker's language and calendar, but keep the
	// service's Loc: "today" and "overdue" are defined in the digest owner's
	// time zone, so the section split and the shown times must agree.
	v.Locale = r.Prefs.Locale
	return h.reply(ctx, m, h.threaded(m, ui.Digest(v, now)))
}

// cmdUnknown answers an unknown command in a private chat.
func (h *Handler) cmdUnknown(ctx context.Context, m *tg.Message, r req, cmd string) error {
	return h.say(ctx, m, r.Prefs.Locale.T("todo.unknown"))
}

// say sends HTML text to the message's chat (as a reply in groups).
func (h *Handler) say(ctx context.Context, m *tg.Message, text string) error {
	return h.reply(ctx, m, h.threaded(m, tg.OutMessage{Text: text}))
}

// threaded makes the reply quote the command in group chats.
func (h *Handler) threaded(m *tg.Message, out tg.OutMessage) tg.OutMessage {
	if m.ChatKind == model.ChatGroup {
		out.ReplyTo = m.ID
	}
	return out
}

func (h *Handler) sayTooLong(ctx context.Context, m *tg.Message, lc i18n.Locale, text string) error {
	return h.say(ctx, m, lc.T("todo.err.toolong", lc.Num(utf8.RuneCountInString(text)), lc.Num(maxTodoChars)))
}

// todoByArg parses a todo number and loads the item from the actor's chat.
// ok=false means the reply (or the unexpected error) is already dealt with.
func (h *Handler) todoByArg(ctx context.Context, m *tg.Message, r req, arg, usageKey string) (td model.Todo, ok bool, err error) {
	lc := r.Prefs.Locale
	n, perr := strconv.Atoi(strings.TrimPrefix(normArg(arg), "#"))
	if perr != nil || n < 1 {
		return td, false, h.say(ctx, m, lc.T(usageKey))
	}
	td, err = h.Svc.ByNum(ctx, r.Actor.ChatID, n)
	if err != nil {
		return td, false, h.todoErr(ctx, m, lc, err)
	}
	return td, true, nil
}

// todoErr turns an expected service error into a localized reply. Anything
// else is returned for the router to log.
func (h *Handler) todoErr(ctx context.Context, m *tg.Message, lc i18n.Locale, err error) error {
	key := ""
	switch {
	case errors.Is(err, service.ErrNotFound):
		key = "todo.err.notfound"
	case errors.Is(err, service.ErrForeignChat):
		key = "todo.err.foreign"
	case errors.Is(err, service.ErrInPast):
		key = "todo.err.past"
	case errors.Is(err, service.ErrEmptyText):
		key = "todo.usage.add"
	default:
		return err
	}
	return h.say(ctx, m, lc.T(key))
}

// itemSummary renders "text — when 🔁" for a freshly stored item.
func (h *Handler) itemSummary(td model.Todo, r req) string {
	s := echo(td.Text)
	if td.DueAt != nil {
		s += " — " + ui.FormatWhen(*td.DueAt, r.Prefs.Locale, tzOf(r), h.Clock.Now())
	}
	if td.Recur != "" {
		s += " 🔁"
		if d := ui.DescribeRule(td.Recur, r.Prefs.Locale); d != "" {
			s += " " + d
		}
	}
	return s + ui.AssigneeSuffix(td, r.Prefs.Locale)
}

// echoRunes is how much of an item's text confirmation replies repeat.
const echoRunes = 200

// echo returns text shortened to echoRunes runes (plus an ellipsis) and
// HTML-escaped. It truncates before escaping, because escaping can expand
// the text up to five times and a reply must stay under Telegram's limit.
func echo(text string) string {
	if r := []rune(text); len(r) > echoRunes {
		text = string(r[:echoRunes]) + "…"
	}
	return ui.Escape(text)
}

func tooLong(s string) bool { return utf8.RuneCountInString(s) > maxTodoChars }

// normArg trims s and converts Persian/Arabic digits to ASCII.
func normArg(s string) string { return strings.TrimSpace(i18n.NormalizeDigits(s)) }

func tzOf(r req) *time.Location {
	if r.Prefs.TZ == nil {
		return time.UTC
	}
	return r.Prefs.TZ
}
