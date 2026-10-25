// Package tgbot adapts github.com/go-telegram/bot to the tg interfaces.
package tgbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

const (
	maxTextUnits    = 4096 // Telegram's limit on message text, in UTF-16 code units
	maxCallbackData = 64   // Telegram's limit on callback_data, in bytes
)

var (
	// ErrMessageTooLong is returned by Send and Edit, without calling the API,
	// when the text exceeds 4096 UTF-16 code units. Text is never split because
	// splitting HTML safely is not possible here; callers paginate.
	ErrMessageTooLong = errors.New("tgbot: message text longer than 4096 UTF-16 code units")
	// ErrCallbackDataTooLong is returned by Send and Edit, without calling the
	// API, when a button's Data exceeds 64 bytes.
	ErrCallbackDataTooLong = errors.New("tgbot: callback data longer than 64 bytes")
	// ErrButtonKind is returned by Send and Edit, without calling the API,
	// when a button sets more than one of Data, URL and WebAppURL.
	ErrButtonKind = errors.New("tgbot: a button may set only one of Data, URL and WebAppURL")
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
	token    string
	username string

	mu      sync.Mutex
	handler tg.Handler // set while Poller().Run is active
}

// New creates the client and calls getMe.
func New(ctx context.Context, token string, opt Options) (*Bot, error) {
	if opt.ServerURL != "" {
		u, err := url.Parse(opt.ServerURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("tgbot: ServerURL must be an http(s) URL with a host")
		}
	}
	w := &Bot{token: token}
	opts := []bot.Option{
		bot.WithSkipGetMe(), // getMe is called below, with our ctx and error mapping
		bot.WithNotAsyncHandlers(),
		// Unbuffered: the library confirms an update to Telegram by asking for a
		// higher offset, and it does so only after the whole fetched batch was
		// handed to the worker. With no buffer, cancelling ctx (lease handover)
		// leaves every update that was not handled unconfirmed, so the next
		// leader receives it again; at most the one in-flight update can be
		// processed twice, which dedupe by update ID absorbs.
		bot.WithUpdatesChannelCap(0),
		bot.WithDefaultHandler(w.dispatch),
		// Polling-loop errors bypass the methods below; redact them here too.
		bot.WithErrorsHandler(func(err error) { slog.Error("telegram client", "err", w.mapErr(err)) }),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message", "callback_query"}),
	}
	if opt.ServerURL != "" {
		opts = append(opts, bot.WithServerURL(opt.ServerURL))
	}
	b, err := bot.New(token, opts...)
	if err != nil {
		return nil, w.mapErr(err)
	}
	w.b = b

	me, err := b.GetMe(ctx)
	if err != nil {
		return nil, fmt.Errorf("tgbot: getMe: %w", w.mapErr(err))
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

// Run polls until ctx is done and calls h synchronously for every update. An
// update whose handler failed is logged and not redelivered. Run returns
// within 1s of ctx being done only if h honours ctx, because the worker is
// synchronous and Run waits for the handler in flight.
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
	return b.mapErr(err)
}

// SetCommands registers the command menu for one language ("" = default,
// "fa", ...).
func (b *Bot) SetCommands(ctx context.Context, cmds []Command, lang string) error {
	list := make([]models.BotCommand, 0, len(cmds))
	for _, c := range cmds {
		list = append(list, models.BotCommand{Command: c.Name, Description: c.Description})
	}
	_, err := b.b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: list, LanguageCode: lang})
	return b.mapErr(err)
}

// SetMenuButton sets the default menu button of every private chat to a
// web_app button with the label text that opens the Mini App at webAppURL.
// The URL must be https, because Telegram refuses other schemes.
func (b *Bot) SetMenuButton(ctx context.Context, text, webAppURL string) error {
	if text == "" {
		return errors.New("tgbot: SetMenuButton: empty text")
	}
	if u, err := url.Parse(webAppURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("tgbot: SetMenuButton: the URL must be https with a host")
	}
	_, err := b.b.SetChatMenuButton(ctx, &bot.SetChatMenuButtonParams{
		MenuButton: models.MenuButtonWebApp{
			Type:   models.MenuButtonTypeWebApp,
			Text:   text,
			WebApp: models.WebAppInfo{URL: webAppURL},
		},
	})
	return b.mapErr(err)
}

// Send sends m (HTML) to chatID and returns the new message's ID.
func (b *Bot) Send(ctx context.Context, chatID int64, m tg.OutMessage) (int, error) {
	if err := validate(m); err != nil {
		return 0, err
	}
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
		return 0, b.mapErr(err)
	}
	return msg.ID, nil
}

// Edit replaces the text and inline keyboard of an existing message. An empty
// keyboard removes it.
func (b *Bot) Edit(ctx context.Context, chatID int64, msgID int, m tg.OutMessage) error {
	if err := validate(m); err != nil {
		return err
	}
	_, err := b.b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        m.Text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: editMarkup(m),
	})
	err = b.mapErr(err)
	var ae *tg.APIError
	if errors.As(err, &ae) && ae.Code == 400 && strings.Contains(ae.Desc, "message is not modified") {
		return nil // a repeated edit is not a failure
	}
	return err
}

// AnswerCallback acknowledges a button press, optionally showing text.
func (b *Bot) AnswerCallback(ctx context.Context, callbackID, text string) error {
	_, err := b.b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: callbackID, Text: text})
	return b.mapErr(err)
}

// validate checks the limits for which Telegram would reject the whole request.
func validate(m tg.OutMessage) error {
	if len(utf16.Encode([]rune(m.Text))) > maxTextUnits {
		return ErrMessageTooLong
	}
	for _, row := range m.Keyboard {
		for _, btn := range row {
			kinds := 0
			for _, s := range []string{btn.Data, btn.URL, btn.WebAppURL} {
				if s != "" {
					kinds++
				}
			}
			if kinds > 1 {
				return fmt.Errorf("%w: %q", ErrButtonKind, btn.Text)
			}
			if len(btn.Data) > maxCallbackData {
				return fmt.Errorf("%w: %q", ErrCallbackDataTooLong, btn.Data)
			}
		}
	}
	return nil
}

// markup builds the reply markup of a sent message. Telegram accepts a single
// reply_markup, so an inline keyboard wins over ForceReply, which wins over
// a reply keyboard, which wins over removing one.
func markup(m tg.OutMessage) models.ReplyMarkup {
	if kb := editMarkup(m); kb != nil {
		return kb
	}
	if m.ForceReply {
		return &models.ForceReply{ForceReply: true}
	}
	if len(m.ReplyKeyboard) > 0 {
		rows := make([][]models.KeyboardButton, len(m.ReplyKeyboard))
		for i, row := range m.ReplyKeyboard {
			rows[i] = make([]models.KeyboardButton, len(row))
			for j, label := range row {
				rows[i][j] = models.KeyboardButton{Text: label}
			}
		}
		return &models.ReplyKeyboardMarkup{Keyboard: rows, IsPersistent: true, ResizeKeyboard: true}
	}
	if m.RemoveKeyboard {
		return &models.ReplyKeyboardRemove{RemoveKeyboard: true}
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
			b := models.InlineKeyboardButton{Text: btn.Text, CallbackData: btn.Data, URL: btn.URL}
			if btn.WebAppURL != "" {
				b.WebApp = &models.WebAppInfo{URL: btn.WebAppURL}
			}
			rows[i][j] = b
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
		out := &tg.Callback{ID: cb.ID, FromID: cb.From.ID, FromUsername: cb.From.Username, FromLang: cb.From.LanguageCode, Data: cb.Data}
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
		out.FromUsername = m.From.Username
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

// mapErr converts a go-telegram/bot error into *tg.APIError and removes the
// bot token from the message. Errors that did not come from the Bot API
// (network, context) keep their chain.
func (b *Bot) mapErr(err error) error {
	if err == nil {
		return nil
	}
	err = convertErr(err)
	var ae *tg.APIError
	if errors.As(err, &ae) {
		ae.Desc = b.redact(ae.Desc)
		return ae
	}
	if msg := err.Error(); b.token != "" && strings.Contains(msg, b.token) {
		return &redactedError{msg: b.redact(msg), err: err}
	}
	return err
}

func (b *Bot) redact(s string) string {
	if b.token == "" {
		return s
	}
	return strings.ReplaceAll(s, b.token, "<token>")
}

// redactedError prints a token-free message and keeps the original chain for
// errors.Is and errors.As.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func convertErr(err error) error {
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
