package handler

import (
	"context"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Owned by #12.

// onCallback handles an inline button press from an allowlisted user. It
// must always answer the callback (AnswerCallback) and must reject actions
// on todos/reminders whose ChatID differs from cb.ChatID.
func (h *Handler) onCallback(ctx context.Context, cb *tg.Callback, r req) error {
	return errNotImplemented
}
