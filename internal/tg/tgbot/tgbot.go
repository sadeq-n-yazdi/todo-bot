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

// Run long-polls until ctx is cancelled, calling h for each update.
func (b *Bot) Run(ctx context.Context, h func(context.Context, tg.Update)) {} // TODO(#6)

// SetCommands registers the command menu.
func (b *Bot) SetCommands(ctx context.Context, cmds []Command) error { return errNotImplemented }

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
