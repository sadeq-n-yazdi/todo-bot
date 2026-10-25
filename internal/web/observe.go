package web

import (
	"context"
	"log/slog"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Observe wraps the bot handler. It calls next for each update and then
// records a membership hint for a group update from an allowlisted user
// (docs/WEB-DESIGN.md §4.3). A hint never grants access: it only makes the
// group a candidate and marks the stored check as stale.
//
// If next returns an error, the update was not recorded, and Observe does
// nothing. A failed hint only gives a warning in the log.
func Observe(next tg.Handler, svc service.Web, allowed map[int64]bool, log *slog.Logger) tg.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return func(ctx context.Context, u tg.Update) error {
		if err := next(ctx, u); err != nil {
			return err
		}
		a, ok := hintActor(u, allowed)
		if !ok {
			return nil
		}
		if err := svc.NoteMember(ctx, a); err != nil {
			log.Warn("web: note member", "chat", a.ChatID, "err", err)
		}
		return nil
	}
}

// hintActor gives the actor for the hint of u, if u is a group message or
// a group button press from an allowlisted user.
func hintActor(u tg.Update, allowed map[int64]bool) (service.Actor, bool) {
	switch {
	case u.Message != nil:
		m := u.Message
		// A migration message comes from the old chat ID, which is going away.
		if m.ChatKind != model.ChatGroup || m.MigrateToChatID != 0 || !allowed[m.FromID] {
			return service.Actor{}, false
		}
		return service.Actor{UserID: m.FromID, ChatID: m.ChatID, ChatKind: model.ChatGroup, ChatTitle: m.ChatTitle}, true
	case u.Callback != nil:
		c := u.Callback
		// A callback has no chat kind; a group has a negative ID. It has no
		// title either, and NoteMember never writes an empty title.
		if c.ChatID >= 0 || !allowed[c.FromID] {
			return service.Actor{}, false
		}
		return service.Actor{UserID: c.FromID, ChatID: c.ChatID, ChatKind: model.ChatGroup}, true
	}
	return service.Actor{}, false
}
