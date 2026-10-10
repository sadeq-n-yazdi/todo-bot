// Package tgbot adapts github.com/go-telegram/bot to the tg interfaces.
package tgbot

import (
	"context"
	"errors"

	"github.com/go-telegram/bot"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Options configures the adapter.
type Options struct {
	ServerURL string // tests point this at an httptest server; "" = api.telegram.org
}

// Command is an entry for setMyCommands.
type Command struct {
	Name        string // without "/"
	Description string
}

// Bot is a long-polling Telegram client implementing tg.Sender.
type Bot struct {
	b        *bot.Bot //nolint:unused // used from #6
	username string
}

var errNotImplemented = errors.New("tgbot: not implemented")

// New creates the client and calls getMe.
func New(ctx context.Context, token string, opt Options) (*Bot, error) {
	return nil, errNotImplemented // TODO(#6)
}

// Username is the bot's @username (without "@").
func (b *Bot) Username() string { return b.username }

// Poller returns a long-polling (getUpdates) tg.Source. Only the node holding
// the "poller" lease runs it; its Run returns within 1s of ctx being done.
// It fills tg.Update.ID from update_id. TODO(#6)
func (b *Bot) Poller() tg.Source { return poller{b} }

type poller struct{ b *Bot }

func (p poller) Run(ctx context.Context, h func(context.Context, tg.Update)) error {
	return errNotImplemented
}

// DeleteWebhook removes any webhook so getUpdates works; the polling leader
// calls it on gaining the lease. TODO(#6)
func (b *Bot) DeleteWebhook(ctx context.Context) error { return errNotImplemented }

// SetCommands registers the command menu for one language ("" = default,
// "fa", ...). TODO(#6)
func (b *Bot) SetCommands(ctx context.Context, cmds []Command, lang string) error {
	return errNotImplemented
}

func (b *Bot) Send(ctx context.Context, chatID int64, m tg.OutMessage) (int, error) {
	return 0, errNotImplemented
}
func (b *Bot) Edit(ctx context.Context, chatID int64, msgID int, m tg.OutMessage) error {
	return errNotImplemented
}
func (b *Bot) AnswerCallback(ctx context.Context, callbackID, text string) error {
	return errNotImplemented
}

var _ tg.Sender = (*Bot)(nil)
