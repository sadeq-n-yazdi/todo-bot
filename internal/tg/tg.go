// Package tg defines library-independent Telegram types. Only
// internal/tg/tgbot imports a Telegram library.
package tg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Message is an incoming text message (or a service message).
type Message struct {
	ID              int
	ChatID          int64
	ChatKind        model.ChatKind // supergroups map to ChatGroup
	ChatTitle       string
	FromID          int64
	FromName        string
	FromUsername    string // sender's @username without "@", as Telegram sends it; "" if none
	FromLang        string // Telegram language_code of the sender, e.g. "fa", "en-GB"
	Text            string // "/cmd@BotName args" is normalised to "/cmd args"
	ReplyToID       int    // ID of the message this replies to, 0 if none
	MigrateToChatID int64  // non-zero on a group -> supergroup migration message
}

// Callback is an inline-keyboard button press.
type Callback struct {
	ID           string
	FromID       int64
	FromUsername string // presser's @username without "@"; "" if none
	FromLang     string
	ChatID       int64
	MessageID    int
	Data         string
}

// Update carries exactly one of Message or Callback.
type Update struct {
	ID       int64 // Telegram update_id; 0 if unknown (tests). Used for dedupe.
	Message  *Message
	Callback *Callback
}

// Button is an inline keyboard button carrying callback data (<= 64 bytes).
type Button struct {
	Text string
	Data string
}

// OutMessage is an outgoing message. Text is HTML (parse_mode=HTML).
//
// Telegram takes one reply markup per message, so at most one of Keyboard,
// ForceReply, ReplyKeyboard and RemoveKeyboard takes effect, in that order
// of precedence. Edit only changes the inline Keyboard: Telegram cannot
// edit the other kinds, and Edit ignores them.
type OutMessage struct {
	Text       string
	Keyboard   [][]Button // inline keyboard
	ForceReply bool
	ReplyTo    int
	// ReplyKeyboard is a persistent, resized keyboard of plain-text
	// buttons under the input field (#68: "➕ New task" in private chats).
	// Pressing a button sends its label as an ordinary text message. It
	// stays until a later message removes or replaces it.
	ReplyKeyboard [][]string
	// RemoveKeyboard removes a ReplyKeyboard sent earlier.
	RemoveKeyboard bool
}

// Sender sends and edits messages.
type Sender interface {
	Send(ctx context.Context, chatID int64, m OutMessage) (msgID int, err error)
	Edit(ctx context.Context, chatID int64, msgID int, m OutMessage) error
	AnswerCallback(ctx context.Context, callbackID, text string) error
}

// APIError is a Telegram Bot API error.
type APIError struct {
	Code       int
	Desc       string
	RetryAfter time.Duration // set on 429
	MigrateTo  int64         // set on 400 "group chat was upgraded to a supergroup"
}

func (e *APIError) Error() string { return fmt.Sprintf("telegram: %d %s", e.Code, e.Desc) }

// IsPermanent reports whether retrying err can never succeed (bot blocked,
// kicked, chat not found). Migration and rate-limit errors are not permanent.
func IsPermanent(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	if ae.MigrateTo != 0 || ae.Code == 429 {
		return false
	}
	return ae.Code == 403 || (ae.Code == 400 && containsAny(ae.Desc, "chat not found", "bot was kicked", "user is deactivated"))
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Handler processes one update. A non-nil error means the update could not
// be recorded (dedupe store unreachable) and should be redelivered if the
// transport can: the webhook answers 500; the poller logs it and moves on.
// Errors while acting on an update are handled (and logged) inside.
type Handler func(context.Context, Update) error

// Source delivers updates to h until ctx is done (long polling in v0.1;
// webhook in v0.2). h is called synchronously, one update at a time.
type Source interface {
	Run(ctx context.Context, h Handler) error
}
