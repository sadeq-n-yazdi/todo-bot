// Package tgtest provides an in-memory tg.Sender for tests.
package tgtest

import (
	"context"
	"sync"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Sent records one Send or Edit call.
type Sent struct {
	ChatID int64
	MsgID  int
	Msg    tg.OutMessage
	Edit   bool
}

// Answer records one AnswerCallback call.
type Answer struct {
	CallbackID string
	Text       string
}

// FakeSender records calls. Set Err (or ErrFn) to make Send fail.
type FakeSender struct {
	mu      sync.Mutex
	nextID  int
	Sent    []Sent
	Answers []Answer
	// ErrFn, if set, is consulted before every Send/Edit; a non-nil result is returned.
	ErrFn func(chatID int64, m tg.OutMessage) error
}

func (f *FakeSender) Send(_ context.Context, chatID int64, m tg.OutMessage) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrFn != nil {
		if err := f.ErrFn(chatID, m); err != nil {
			return 0, err
		}
	}
	f.nextID++
	f.Sent = append(f.Sent, Sent{ChatID: chatID, MsgID: f.nextID, Msg: m})
	return f.nextID, nil
}

func (f *FakeSender) Edit(_ context.Context, chatID int64, msgID int, m tg.OutMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ErrFn != nil {
		if err := f.ErrFn(chatID, m); err != nil {
			return err
		}
	}
	f.Sent = append(f.Sent, Sent{ChatID: chatID, MsgID: msgID, Msg: m, Edit: true})
	return nil
}

func (f *FakeSender) AnswerCallback(_ context.Context, callbackID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Answers = append(f.Answers, Answer{CallbackID: callbackID, Text: text})
	return nil
}

// Messages returns a copy of all recorded sends and edits.
func (f *FakeSender) Messages() []Sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Sent(nil), f.Sent...)
}

// Last returns the most recent send or edit, ok=false if none.
func (f *FakeSender) Last() (Sent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Sent) == 0 {
		return Sent{}, false
	}
	return f.Sent[len(f.Sent)-1], true
}

// Reset clears recorded calls.
func (f *FakeSender) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent, f.Answers = nil, nil
}

var _ tg.Sender = (*FakeSender)(nil)
