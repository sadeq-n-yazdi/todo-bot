package handler

import (
	"context"
	"errors"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Owned by #11. Each command replies to the user itself for expected errors
// (bad args, not found) and returns nil; it returns an error only for
// unexpected failures.

var errNotImplemented = errors.New("handler: not implemented")

func (h *Handler) cmdStart(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdHelp(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdAdd(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdList(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdDone(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdDel(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdEdit(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}
func (h *Handler) cmdToday(ctx context.Context, m *tg.Message, r req, args string) error {
	return errNotImplemented
}

// cmdUnknown answers an unknown command in a private chat.
func (h *Handler) cmdUnknown(ctx context.Context, m *tg.Message, r req, cmd string) error {
	return errNotImplemented
}
