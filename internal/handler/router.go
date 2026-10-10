// Package handler turns Telegram updates into service calls and replies.
//
// File ownership (parallel work): router.go and messages_router.go (#1),
// cmd_todo.go + messages_todo.go (#11), callbacks.go, flows.go,
// cmd_settings.go + messages_settings.go (#12).
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Deps are the handler's collaborators.
type Deps struct {
	Svc         service.Service
	Send        tg.Sender
	Parser      nlparse.Parser
	Clock       clock.Clock
	Allowed     map[int64]bool
	BotUsername string
	Log         *slog.Logger
}

// Handler dispatches updates. Handle is safe for concurrent use.
type Handler struct {
	Deps
}

// New returns a Handler.
func New(d Deps) *Handler {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Handler{Deps: d}
}

// req is the context every command, callback and flow receives.
type req struct {
	Actor service.Actor
	Prefs model.Prefs
}

type cmdFunc func(h *Handler, ctx context.Context, m *tg.Message, r req, args string) error

// commands maps command names (without "/") to implementations.
var commands = map[string]cmdFunc{
	// cmd_todo.go (#11)
	"start": (*Handler).cmdStart,
	"help":  (*Handler).cmdHelp,
	"add":   (*Handler).cmdAdd,
	"list":  (*Handler).cmdList,
	"done":  (*Handler).cmdDone,
	"del":   (*Handler).cmdDel,
	"edit":  (*Handler).cmdEdit,
	"today": (*Handler).cmdToday,
	// cmd_settings.go (#12)
	"remind":   (*Handler).cmdRemind,
	"tz":       (*Handler).cmdTZ,
	"digest":   (*Handler).cmdDigest,
	"lang":     (*Handler).cmdLang,
	"calendar": (*Handler).cmdCalendar,
}

// Handle processes one update. It never panics.
func (h *Handler) Handle(ctx context.Context, u tg.Update) {
	defer func() {
		if p := recover(); p != nil {
			h.Log.Error("handler panic", "panic", p, "stack", string(debug.Stack()))
		}
	}()
	switch {
	case u.Callback != nil:
		h.handleCallback(ctx, u.Callback)
	case u.Message != nil:
		h.handleMessage(ctx, u.Message)
	}
}

func (h *Handler) handleCallback(ctx context.Context, cb *tg.Callback) {
	if !h.Allowed[cb.FromID] {
		_ = h.Send.AnswerCallback(ctx, cb.ID, "")
		return
	}
	kind := model.ChatPrivate
	if cb.ChatID < 0 {
		kind = model.ChatGroup
	}
	r, err := h.newReq(ctx, cb.FromID, cb.FromLang, cb.ChatID, kind, "")
	if err != nil {
		h.Log.Error("prefs", "user", cb.FromID, "err", err)
		_ = h.Send.AnswerCallback(ctx, cb.ID, "")
		return
	}
	if err := h.onCallback(ctx, cb, r); err != nil {
		h.Log.Error("callback", "data", cb.Data, "err", err)
		_ = h.Send.AnswerCallback(ctx, cb.ID, r.Prefs.Locale.T("error.generic"))
	}
}

func (h *Handler) handleMessage(ctx context.Context, m *tg.Message) {
	if m.MigrateToChatID != 0 {
		if err := h.onMigrate(ctx, m); err != nil {
			h.Log.Error("migrate", "from", m.ChatID, "to", m.MigrateToChatID, "err", err)
		}
		return
	}
	cmd, args, isCmd := splitCommand(m.Text, h.BotUsername)
	if isCmd && cmd == "whoami" && m.ChatKind == model.ChatPrivate {
		// The only command answered for non-allowlisted users.
		_, err := h.Send.Send(ctx, m.ChatID, tg.OutMessage{Text: fmt.Sprintf("<code>%d</code>", m.FromID)})
		h.logErr("whoami", err)
		return
	}
	if !h.Allowed[m.FromID] {
		return
	}
	r, err := h.newReq(ctx, m.FromID, m.FromLang, m.ChatID, m.ChatKind, m.ChatTitle)
	if err != nil {
		h.Log.Error("prefs", "user", m.FromID, "err", err)
		return
	}
	if m.ReplyToID != 0 {
		handled, err := h.onReply(ctx, m, r)
		if handled {
			h.replyErr(ctx, m, r, "reply", err)
			return
		}
	}
	switch {
	case isCmd:
		f, ok := commands[cmd]
		if !ok {
			if m.ChatKind == model.ChatPrivate {
				h.replyErr(ctx, m, r, cmd, h.cmdUnknown(ctx, m, r, cmd))
			}
			return
		}
		h.replyErr(ctx, m, r, cmd, f(h, ctx, m, r, args))
	case m.ChatKind == model.ChatPrivate && strings.TrimSpace(m.Text) != "":
		h.replyErr(ctx, m, r, "add", h.cmdAdd(ctx, m, r, m.Text))
	}
}

func (h *Handler) newReq(ctx context.Context, userID int64, tgLang string, chatID int64, kind model.ChatKind, title string) (req, error) {
	p, err := h.Svc.Prefs(ctx, userID, tgLang)
	if err != nil {
		return req{}, err
	}
	return req{
		Actor: service.Actor{UserID: userID, ChatID: chatID, ChatKind: kind, ChatTitle: title},
		Prefs: p,
	}, nil
}

// replyErr logs err and tells the user something went wrong. Commands are
// expected to reply themselves for expected, user-facing errors and return
// nil; a non-nil error here is unexpected.
func (h *Handler) replyErr(ctx context.Context, m *tg.Message, r req, what string, err error) {
	if err == nil {
		return
	}
	h.Log.Error("command failed", "cmd", what, "chat", m.ChatID, "err", err)
	_, sendErr := h.Send.Send(ctx, m.ChatID, tg.OutMessage{Text: r.Prefs.Locale.T("error.generic"), ReplyTo: m.ID})
	h.logErr("send error reply", sendErr)
}

func (h *Handler) logErr(what string, err error) {
	if err != nil {
		h.Log.Warn(what, "err", err)
	}
}

// reply sends text (HTML) to the message's chat.
//
//nolint:unused // used by cmd_todo.go / cmd_settings.go (#11, #12)
func (h *Handler) reply(ctx context.Context, m *tg.Message, out tg.OutMessage) error {
	_, err := h.Send.Send(ctx, m.ChatID, out)
	return err
}

// splitCommand parses "/cmd@bot args". Commands addressed to another bot
// are not commands for us.
func splitCommand(text, botUsername string) (cmd, args string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	head, rest, _ := strings.Cut(text[1:], " ")
	if name, at, found := strings.Cut(head, "@"); found {
		if !strings.EqualFold(at, botUsername) {
			return "", "", false
		}
		head = name
	}
	if head == "" {
		return "", "", false
	}
	return strings.ToLower(head), strings.TrimSpace(rest), true
}
