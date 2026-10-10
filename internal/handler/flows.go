package handler

import (
	"context"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Owned by #12: ForceReply prompt flows (edit text, set reminder time) and
// group -> supergroup migration.

// prompts tracks pending ForceReply prompts keyed by (chatID, promptMsgID),
// with a TTL. Must be safe for concurrent use.
type prompts struct{}

func newPrompts() *prompts { return &prompts{} }

// onReply handles a reply to one of our prompts. handled=false means the
// message was not a reply to a pending prompt and should be processed
// normally.
func (h *Handler) onReply(ctx context.Context, m *tg.Message, r req) (handled bool, err error) {
	return false, nil
}

// onMigrate handles a migrate_to_chat_id service message.
func (h *Handler) onMigrate(ctx context.Context, m *tg.Message) error {
	return errNotImplemented
}
