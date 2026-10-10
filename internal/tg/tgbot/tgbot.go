// Package tgbot adapts github.com/go-telegram/bot to the tg interfaces.
package tgbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
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
	b        *bot.Bot
	username string

	mu      sync.Mutex
	handler tg.Handler // set while Poller().Run is active
}

// New creates the client and calls getMe.
func New(ctx context.Context, token string, opt Options) (*Bot, error) {
	w := &Bot{}
	opts := []bot.Option{
		bot.WithSkipGetMe(), // getMe is called below, with our ctx and error mapping
		bot.WithNotAsyncHandlers(),
		bot.WithDefaultHandler(w.dispatch),
		bot.WithErrorsHandler(func(err error) { slog.Error("telegram client", "err", err) }),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message", "callback_query"}),
	}
	if opt.ServerURL != "" {
		opts = append(opts, bot.WithServerURL(opt.ServerURL))
	}
	b, err := bot.New(token, opts...)
	if err != nil {
		return nil, err
	}
	w.b = b

	me, err := b.GetMe(ctx)
	if err != nil {
		return nil, fmt.Errorf("tgbot: getMe: %w", mapErr(err))
	}
	w.username = me.Username
	return w, nil
}

// Username is the bot's @username (without "@").
func (b *Bot) Username() string { return b.username }

// Poller returns a long-polling (getUpdates) tg.Source. Only the node holding
// the "poller" lease runs it; its Run returns within 1s of ctx being done.
// It fills tg.Update.ID from update_id.
func (b *Bot) Poller() tg.Source { return poller{b} }

type poller struct{ b *Bot }

// Run polls until ctx is done and calls h synchronously for every update. The
// library moves the offset forward before h runs, so an update whose handler
// failed is logged and not redelivered.
func (p poller) Run(ctx context.Context, h tg.Handler) error {
	p.b.mu.Lock()
	if p.b.handler != nil {
		p.b.mu.Unlock()
		return errors.New("tgbot: poller already running")
	}
	p.b.handler = h
	p.b.mu.Unlock()
	defer func() {
		p.b.mu.Lock()
		p.b.handler = nil
		p.b.mu.Unlock()
	}()

	p.b.b.Start(ctx)
	return nil
}

// dispatch is the library's default handler: it converts the update and hands
// it to the handler of the active Run.
func (b *Bot) dispatch(ctx context.Context, _ *bot.Bot, upd *models.Update) {
	b.mu.Lock()
	h := b.handler
	b.mu.Unlock()
	if h == nil {
		return
	}
	u, ok := b.convert(upd)
	if !ok {
		return
	}
	if err := h(ctx, u); err != nil {
		slog.Error("telegram update handler failed", "update_id", u.ID, "err", err)
	}
}

// DeleteWebhook removes any webhook so getUpdates works; the polling leader
// calls it on gaining the lease.
func (b *Bot) DeleteWebhook(ctx context.Context) error {
	_, err := b.b.DeleteWebhook(ctx, &bot.DeleteWebhookParams{})
	return mapErr(err)
}

// SetCommands registers the command menu for one language ("" = default,
// "fa", ...).
func (b *Bot) SetCommands(ctx context.Context, cmds []Command, lang string) error {
	list := make([]models.BotCommand, 0, len(cmds))
	for _, c := range cmds {
		list = append(list, models.BotCommand{Command: c.Name, Description: c.Description})
	}
	_, err := b.b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: list, LanguageCode: lang})
	return mapErr(err)
}

// Send sends m (HTML) to chatID and returns the new message's ID.
func (b *Bot) Send(ctx context.Context, chatID int64, m tg.OutMessage) (int, error) {
	p := &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        m.Text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: markup(m),
	}
	if m.ReplyTo != 0 {
		p.ReplyParameters = &models.ReplyParameters{MessageID: m.ReplyTo, AllowSendingWithoutReply: true}
	}
	msg, err := b.b.SendMessage(ctx, p)
	if err != nil {
		return 0, mapErr(err)
	}
	return msg.ID, nil
}

// Edit replaces the text and inline keyboard of an existing message. An empty
// keyboard removes it.
func (b *Bot) Edit(ctx context.Context, chatID int64, msgID int, m tg.OutMessage) error {
	_, err := b.b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        m.Text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: editMarkup(m),
	})
	return mapErr(err)
}

// AnswerCallback acknowledges a button press, optionally showing text.
func (b *Bot) AnswerCallback(ctx context.Context, callbackID, text string) error {
	_, err := b.b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: callbackID, Text: text})
	return mapErr(err)
}

// markup builds the reply markup of a sent message. Telegram accepts a single
// reply_markup, so an inline keyboard wins over ForceReply.
func markup(m tg.OutMessage) models.ReplyMarkup {
	if kb := editMarkup(m); kb != nil {
		return kb
	}
	if m.ForceReply {
		return &models.ForceReply{ForceReply: true}
	}
	return nil
}

// editMarkup returns the inline keyboard of m, or a nil interface if it has none.
func editMarkup(m tg.OutMessage) models.ReplyMarkup {
	if len(m.Keyboard) == 0 {
		return nil
	}
	rows := make([][]models.InlineKeyboardButton, len(m.Keyboard))
	for i, row := range m.Keyboard {
		rows[i] = make([]models.InlineKeyboardButton, len(row))
		for j, btn := range row {
			rows[i][j] = models.InlineKeyboardButton{Text: btn.Text, CallbackData: btn.Data}
		}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// convert maps a library update to a tg.Update; ok is false for update kinds
// the bot does not handle.
func (b *Bot) convert(u *models.Update) (tg.Update, bool) {
	switch {
	case u.Message != nil:
		return tg.Update{ID: u.ID, Message: b.convertMessage(u.Message)}, true
	case u.CallbackQuery != nil:
		cb := u.CallbackQuery
		out := &tg.Callback{ID: cb.ID, FromID: cb.From.ID, FromLang: cb.From.LanguageCode, Data: cb.Data}
		switch {
		case cb.Message.Message != nil:
			out.ChatID, out.MessageID = cb.Message.Message.Chat.ID, cb.Message.Message.ID
		case cb.Message.InaccessibleMessage != nil:
			out.ChatID, out.MessageID = cb.Message.InaccessibleMessage.Chat.ID, cb.Message.InaccessibleMessage.MessageID
		}
		return tg.Update{ID: u.ID, Callback: out}, true
	}
	return tg.Update{}, false
}

func (b *Bot) convertMessage(m *models.Message) *tg.Message {
	out := &tg.Message{
		ID:              m.ID,
		ChatID:          m.Chat.ID,
		ChatKind:        chatKind(m.Chat.Type),
		ChatTitle:       m.Chat.Title,
		Text:            normalizeCommand(m.Text, b.username),
		MigrateToChatID: m.MigrateToChatID,
	}
	if m.From != nil {
		out.FromID = m.From.ID
		out.FromLang = m.From.LanguageCode
		out.FromName = displayName(m.From)
	}
	if m.ReplyToMessage != nil {
		out.ReplyToID = m.ReplyToMessage.ID
	}
	return out
}

func displayName(u *models.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return name
}

func chatKind(t models.ChatType) model.ChatKind {
	if t == models.ChatTypePrivate {
		return model.ChatPrivate
	}
	return model.ChatGroup
}

// normalizeCommand turns "/cmd@BotName args" into "/cmd args". Commands
// addressed to another bot are left as they are.
func normalizeCommand(text, username string) string {
	if !strings.HasPrefix(text, "/") || username == "" {
		return text
	}
	end := strings.IndexAny(text, " \t\r\n")
	if end < 0 {
		end = len(text)
	}
	cmd, rest := text[:end], text[end:]
	at := strings.IndexByte(cmd, '@')
	if at < 0 || !strings.EqualFold(cmd[at+1:], username) {
		return text
	}
	return cmd[:at] + rest
}

var rawErrRE = regexp.MustCompile(`, (\d{3}) (.*)$`)

// mapErr converts a go-telegram/bot error into *tg.APIError. Errors that did
// not come from the Bot API (network, context) are returned unchanged.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var tooMany *bot.TooManyRequestsError
	var migrate *bot.MigrateError
	switch {
	case errors.As(err, &tooMany):
		return &tg.APIError{Code: 429, Desc: tooMany.Message, RetryAfter: time.Duration(tooMany.RetryAfter) * time.Second}
	case errors.As(err, &migrate):
		return &tg.APIError{Code: 400, Desc: migrate.Message, MigrateTo: int64(migrate.MigrateToChatID)}
	}
	for _, s := range []struct {
		sentinel error
		code     int
	}{
		{bot.ErrorBadRequest, 400},
		{bot.ErrorUnauthorized, 401},
		{bot.ErrorForbidden, 403},
		{bot.ErrorNotFound, 404},
		{bot.ErrorConflict, 409},
	} {
		if errors.Is(err, s.sentinel) {
			// The library formats these as "<sentinel>, <description>".
			return &tg.APIError{Code: s.code, Desc: strings.TrimPrefix(err.Error(), s.sentinel.Error()+", ")}
		}
	}
	// Other codes: "error response from telegram for method X, 502 Bad Gateway".
	if m := rawErrRE.FindStringSubmatch(err.Error()); m != nil && strings.HasPrefix(err.Error(), "error response from telegram") {
		var code int
		_, _ = fmt.Sscanf(m[1], "%d", &code)
		return &tg.APIError{Code: code, Desc: m[2]}
	}
	return err
}

var _ tg.Sender = (*Bot)(nil)
