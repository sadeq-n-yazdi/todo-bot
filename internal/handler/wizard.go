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
