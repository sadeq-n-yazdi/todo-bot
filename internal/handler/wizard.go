package handler

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Step-by-step task creation wizard (#68, ARCHITECTURE §12).
//
// The flow keeps no state in memory: every step reads the draft through
// Svc.Draft / Svc.DraftByMessage and writes it with Svc.UpdateDraft (a
// compare-and-set on Draft.Version), so any node can continue a wizard.
// This file holds the entry points, text answers and the reply keyboard;
// wizard_flow.go holds the button presses and wizard_save.go the Save
// mapping.

// cmdNew starts the wizard (/new). Bare /add, the ➕ button under /list
// and the reply-keyboard label start it too.
func (h *Handler) cmdNew(ctx context.Context, m *tg.Message, r req, _ string) error {
	return h.startWizard(ctx, r, m.ChatID, m.ID)
}

// startWizard starts a new draft for (chat, actor), replacing an earlier
// one. replyTo is the message to quote in a group (0 = none).
//
// The card is first sent with a placeholder inline row (one ActWizNoop
// button, data "wz:0"): the card is edited at every step, and Telegram
// cannot edit a message that carries a reply keyboard. The version that the
// real buttons must carry is known only after StartDraft, so the card is
// edited to the first step afterwards. It is sent with h.Send directly, so
// the reply-keyboard hook never sees it.
func (h *Handler) startWizard(ctx context.Context, r req, chatID int64, replyTo int) error {
	lc := r.Prefs.Locale
	group := r.Actor.ChatKind == model.ChatGroup
	card := tg.OutMessage{
		Text:     lc.T("wizard.starting"),
		Keyboard: [][]tg.Button{{{Text: "⏳", Data: ui.Encode(ui.Action{Kind: ui.ActWizNoop})}}},
	}
	if group {
		card.ReplyTo = replyTo
	}
	cardID, err := h.Send.Send(ctx, chatID, card)
	if err != nil {
		return err
	}
	d := model.Draft{ChatID: chatID, UserID: r.Actor.UserID, MsgID: cardID, Step: model.StepText}
	if group {
		if d.AskMsgID, err = h.sendAsk(ctx, r, d); err != nil {
			return err
		}
	}
	if err := h.Svc.StartDraft(ctx, &d); err != nil {
		return err
	}
	h.draw(ctx, r, d, cardOpts{})
	return nil
}

// sendAsk sends the ForceReply question of a text step in a group, as a
// reply to the card, and returns its message ID.
func (h *Handler) sendAsk(ctx context.Context, r req, d model.Draft) (int, error) {
	lc := r.Prefs.Locale
	out := ui.WizardAsk(d.Step, lc)
	if out.Text == "" {
		out.Text = lc.T("wizard.ask." + string(d.Step))
	}
	out.ForceReply = true
	out.ReplyTo = d.MsgID
	return h.Send.Send(ctx, d.ChatID, out)
}

// onWizardText handles a non-command text message before it is treated as
// a new item: the reply-keyboard label (ui.IsNewTaskLabel) starts the
// wizard, and a text answer to the sender's draft in this chat (a reply to
// its card or question; in a private chat any text while the draft waits
// for text) completes the step. handled=false means the message has
// nothing to do with a wizard and is processed as before.
func (h *Handler) onWizardText(ctx context.Context, m *tg.Message, r req) (handled bool, err error) {
	private := m.ChatKind == model.ChatPrivate
	if private && ui.IsNewTaskLabel(m.Text) {
		return true, h.startWizard(ctx, r, m.ChatID, 0)
	}
	// Ordinary group chatter must cost no query.
	if !private && m.ReplyToID == 0 {
		return false, nil
	}
	d, err := h.Svc.Draft(ctx, m.ChatID, m.FromID)
	if errors.Is(err, service.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if d.Step != model.StepText && d.Step != model.StepTimeText {
		return false, nil
	}
	if !private && m.ReplyToID != d.MsgID && m.ReplyToID != d.AskMsgID {
		return false, nil
	}
	nd, opts, err := h.answerText(r, d, m.Text)
	if err != nil {
		return true, err
	}
	stale, err := h.commit(ctx, r, d, nd, opts)
	if err != nil {
		return true, err
	}
	if stale {
		h.redrawStored(ctx, r, d.ChatID, d.MsgID)
	}
	return true, nil
}

// ---- the persistent "➕ New task" reply keyboard (§12) ----
//
// Telegram attaches a reply keyboard only to a sent message (not to an
// edit, a callback answer, or a message that carries an inline keyboard or
// ForceReply). So the keyboard rides on the next plain reply in the user's
// private chat while the user's stored version (r.Prefs.KBVersion) is not
// ui.NewTaskKeyboardVersion. handleMessage marks the request with
// newTaskKeyboardCtx; reply, the shared helper behind say and every
// command's answer, asks offerNewTaskKeyboard before each send. The mark is
// request-scoped (a value in ctx), never shared between updates or nodes:
// what a user has is known only from users.kb_version.

type kbMarkKey struct{}

// kbMark is the use-once mark of one update in a private chat.
type kbMark struct {
	userID int64
	lc     i18n.Locale
	stale  bool // the stored version differs: the keyboard is due
	used   atomic.Bool
}

// newTaskKeyboardCtx returns ctx marked (a use-once pointer) so that the
// request's first plain reply carries the keyboard, if m is in a private
// chat. The mark is set even when the user's version is current: a
// keyboard that a command attaches itself (/start, /help, /lang) then uses
// it up too, and a second message of the update cannot carry another.
// In a group ctx is returned unchanged.
func (h *Handler) newTaskKeyboardCtx(ctx context.Context, m *tg.Message, r req) context.Context {
	if m.ChatKind != model.ChatPrivate {
		return ctx
	}
	return context.WithValue(ctx, kbMarkKey{}, &kbMark{
		userID: r.Actor.UserID,
		lc:     r.Prefs.Locale,
		stale:  r.Prefs.KBVersion != ui.NewTaskKeyboardVersion,
	})
}

// offerNewTaskKeyboard is called by reply before it sends out. If ctx is
// marked and the mark is unused, and out has no Keyboard, ForceReply or
// RemoveKeyboard, it sets out.ReplyKeyboard to ui.NewTaskKeyboard in the
// user's language (when the user's version is out of date) and uses the
// mark. It also accepts an out whose ReplyKeyboard the caller set itself.
//
// Rule for callers: a message that will be edited later must never carry a
// reply keyboard, because Telegram cannot edit a message that has one. So
// such a message is sent with an inline keyboard (which makes the hook
// skip it) or with h.Send.Send directly.
//
// When the user's version was out of date, it returns a function that reply
// calls once the send succeeded, which stores ui.NewTaskKeyboardVersion
// with Svc.SetKBVersion (an error is only logged). Otherwise it returns nil.
func (h *Handler) offerNewTaskKeyboard(ctx context.Context, out *tg.OutMessage) (sent func()) {
	mk, _ := ctx.Value(kbMarkKey{}).(*kbMark)
	if mk == nil || out.Keyboard != nil || out.ForceReply || out.RemoveKeyboard {
		return nil
	}
	if out.ReplyKeyboard == nil {
		if !mk.stale || mk.used.Swap(true) {
			return nil
		}
		out.ReplyKeyboard = ui.NewTaskKeyboard(mk.lc)
	} else if mk.used.Swap(true) {
		return nil
	}
	if !mk.stale {
		return nil
	}
	return func() {
		if err := h.Svc.SetKBVersion(ctx, mk.userID, ui.NewTaskKeyboardVersion); err != nil {
			h.Log.Warn("set keyboard version", "user", mk.userID, "err", err)
		}
	}
}

// sayWithKeyboard sends text as a reply; in a private chat it carries the
// "➕ New task" keyboard, even when the user has it already.
func (h *Handler) sayWithKeyboard(ctx context.Context, m *tg.Message, lc i18n.Locale, text string) error {
	out := tg.OutMessage{Text: text}
	if m.ChatKind == model.ChatPrivate {
		out.ReplyKeyboard = ui.NewTaskKeyboard(lc)
	}
	return h.reply(ctx, m, h.threaded(m, out))
}
