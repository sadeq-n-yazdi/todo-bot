package handler

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Assignment (#58, ARCHITECTURE §11): the leading @username of /add, the
// "Assigned to me" section of /list, and the "did" notice.

// leadingMention matches the first token of an /add text: "@name", with an
// optional trailing "," or ":" ("@wife: buy milk").
var leadingMention = regexp.MustCompile(`^@([A-Za-z][A-Za-z0-9_]{4,31})[,:]?$`)

// splitMention splits a leading @username off an /add text. It returns the
// name (without "@") and the remaining text. name is "" when there is no
// assignee: the text does not start with a mention, the mention is the whole
// text, or it names the bot itself (then it is dropped from the text).
func splitMention(input, botUsername string) (name, rest string) {
	input = strings.TrimSpace(input)
	head, tail := input, ""
	if i := strings.IndexFunc(input, unicode.IsSpace); i >= 0 {
		head, tail = input[:i], strings.TrimSpace(input[i:])
	}
	m := leadingMention.FindStringSubmatch(i18n.NormalizeDigits(head))
	if m == nil || tail == "" {
		return "", input
	}
	if strings.EqualFold(m[1], botUsername) {
		return "", tail
	}
	return m[1], tail
}

// resolveAssignee looks name up. ok=false means nobody who may be assigned
// items has that name (unknown, or not allowlisted); err is an unexpected
// failure.
func (h *Handler) resolveAssignee(ctx context.Context, name string) (u model.User, ok bool, err error) {
	u, err = h.Svc.UserByUsername(ctx, name)
	if errors.Is(err, service.ErrNotFound) {
		return model.User{}, false, nil
	}
	if err != nil {
		return model.User{}, false, err
	}
	return u, h.Allowed[u.ID], nil
}

// renderList renders a page of chatID's list. In a private chat, the first
// page also shows the items assigned to the user in other chats. If those
// cannot be loaded, the list is shown without them.
func (h *Handler) renderList(ctx context.Context, r req, chatID int64, ts []model.Todo, page, pages int) tg.OutMessage {
	var assigned []model.Todo
	if page == 0 && r.Actor.ChatKind == model.ChatPrivate && chatID == r.Actor.ChatID {
		var err error
		if assigned, err = h.Svc.ListAssigned(ctx, r.Actor.UserID); err != nil {
			h.Log.Warn("list assigned", "user", r.Actor.UserID, "err", err)
			assigned = nil
		}
	}
	out := ui.TodoListAssigned(ts, assigned, r.Prefs.Locale, tzOf(r), h.Clock.Now(), page, pages)
	out.Keyboard = append(out.Keyboard, []tg.Button{ui.NewTaskButton(r.Prefs.Locale)})
	return out
}

// notifyDid tells the creating chat that its assignee completed done. It
// does nothing unless the actor is the item's assignee acting from another
// chat than the item's own. The text is in the creator's language. A failure
// is only logged: the item is already done.
func (h *Handler) notifyDid(ctx context.Context, r req, done model.Todo) {
	if done.AssigneeID == 0 || done.AssigneeID != r.Actor.UserID || done.ChatID == r.Actor.ChatID {
		return
	}
	lc := i18n.Default("")
	if p, err := h.Svc.Prefs(ctx, done.CreatedBy, ""); err != nil {
		h.Log.Warn("did notice prefs", "user", done.CreatedBy, "err", err)
	} else {
		lc = p.Locale
	}
	text := lc.T("todo.assign.did", ui.Mention(done.AssigneeID, done.AssigneeName, lc), echo(done.Text))
	_, err := h.Send.Send(ctx, done.ChatID, tg.OutMessage{Text: text})
	h.logErr("did notice", err)
}
