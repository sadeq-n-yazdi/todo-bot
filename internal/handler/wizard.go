package handler

import (
	"context"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Step-by-step task creation wizard (#68, ARCHITECTURE §12).
//
// The flow is stateless in memory: every step reads the draft through
// Svc.Draft / Svc.DraftByMessage and writes it with Svc.UpdateDraft (a
// compare-and-set on Draft.Version), so any node can continue a wizard.
//
// TODO(#68): the handler follow-up PR implements these. The stubs below
// keep today's behaviour and call no service method, so the existing
// handler fakes keep working until that PR adds the draft methods to them.

// cmdNew starts the wizard (/new). Bare /add, the ➕ button under /list
// and the reply-keyboard label start it too.
func (h *Handler) cmdNew(ctx context.Context, m *tg.Message, r req, _ string) error {
	// Until then /new answers as an unknown command did before.
	if m.ChatKind != model.ChatPrivate {
		return nil
	}
	return h.cmdUnknown(ctx, m, r, "new")
}

// onWizardText handles a non-command text message before it is treated as
// a new item: the reply-keyboard label (ui.IsNewTaskLabel) starts the
// wizard, and a text answer to the sender's draft in this chat (a reply to
// its card or question; in a private chat any text while the draft waits
// for text) completes the step. handled=false means the message has
// nothing to do with a wizard and is processed as before.
func (h *Handler) onWizardText(ctx context.Context, m *tg.Message, r req) (handled bool, err error) {
	_, _, _ = ctx, m, r
	return false, nil
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

// newTaskKeyboardCtx returns ctx marked (a use-once pointer) so that the
// request's first plain reply carries the keyboard, if m is in a private
// chat and the user's KBVersion is out of date. Otherwise it returns ctx
// unchanged.
//
// TODO(#68): the handler follow-up PR implements it.
func (h *Handler) newTaskKeyboardCtx(ctx context.Context, m *tg.Message, r req) context.Context {
	_, _ = m, r
	return ctx
}

// offerNewTaskKeyboard is called by reply before it sends out. If ctx is
// marked and the mark is unused, and out has no Keyboard, ForceReply or
// RemoveKeyboard, it sets out.ReplyKeyboard to ui.NewTaskKeyboard in the
// user's language and uses the mark. The mark is a use-once pointer stored
// in ctx, so only one message per update carries it.
//
// Rule for callers: a message that will be edited later must never carry a
// reply keyboard, because Telegram cannot edit a message that has one. So
// such a message is sent with an inline keyboard (which makes the hook
// skip it) or with h.Send.Send directly. The wizard's start card is sent
// with a placeholder inline row (one ui.ActWizNoop button, data "wz:0")
// and then edited to the first step. It also accepts an out whose
// ReplyKeyboard the caller set itself (/start, /help, /lang). In both
// cases it returns a function that reply calls once the send succeeded,
// which stores ui.NewTaskKeyboardVersion with Svc.SetKBVersion (an error
// is only logged). Otherwise it returns nil.
//
// TODO(#68): the handler follow-up PR implements it.
func (h *Handler) offerNewTaskKeyboard(ctx context.Context, out *tg.OutMessage) (sent func()) {
	_, _ = ctx, out
	return nil
}
