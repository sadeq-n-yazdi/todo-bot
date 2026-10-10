package handler

import (
	"context"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Owned by #12: ForceReply prompt flows (edit text, set reminder time) and
// group -> supergroup migration.

// Pending prompts are persisted via Svc.PutPrompt / Svc.TakePrompt so that
// any node, or a restarted process, can complete them.

// onReply handles a reply to one of our prompts (Svc.TakePrompt on
// m.ChatID, m.ReplyToID). handled=false means the
// message was not a reply to a pending prompt and should be processed
// normally.
func (h *Handler) onReply(ctx context.Context, m *tg.Message, r req) (handled bool, err error) {
	return false, nil
}

// onMigrate handles a migrate_to_chat_id service message.
func (h *Handler) onMigrate(ctx context.Context, m *tg.Message) error {
	return errNotImplemented
}
