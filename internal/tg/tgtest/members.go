package tgtest

import (
	"context"
	"sync"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// MemberCall records one ChatMember call.
type MemberCall struct {
	ChatID, UserID int64
}

type memberKey struct{ chat, user int64 }

// FakeMembers is an in-memory tg.MemberChecker. It records every call. A pair
// with no answer set gets Err if that is set, else Default.
type FakeMembers struct {
	mu      sync.Mutex
	answers map[memberKey]tg.ChatMember
	errs    map[memberKey]error
	calls   []MemberCall
	// Default is the answer for a pair that has none.
	Default tg.ChatMember
	// Err, if set, is returned for a pair that has no answer.
	Err error
	// Hook, if set, runs at the start of every call, before the call is
	// recorded. Tests use it to block a call or to count calls in flight.
	Hook func(ctx context.Context, chatID, userID int64)
}

// Set makes ChatMember(chatID, userID) return m.
func (f *FakeMembers) Set(chatID, userID int64, m tg.ChatMember) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.answers == nil {
		f.answers = map[memberKey]tg.ChatMember{}
	}
	k := memberKey{chatID, userID}
	f.answers[k] = m
	delete(f.errs, k)
}

// SetErr makes ChatMember(chatID, userID) return err.
func (f *FakeMembers) SetErr(chatID, userID int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errs == nil {
		f.errs = map[memberKey]error{}
	}
	k := memberKey{chatID, userID}
	f.errs[k] = err
	delete(f.answers, k)
}

// ChatMember implements tg.MemberChecker.
func (f *FakeMembers) ChatMember(ctx context.Context, chatID, userID int64) (tg.ChatMember, error) {
	if f.Hook != nil {
		f.Hook(ctx, chatID, userID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, MemberCall{chatID, userID})
	k := memberKey{chatID, userID}
	if err, ok := f.errs[k]; ok {
		return tg.ChatMember{}, err
	}
	if m, ok := f.answers[k]; ok {
		return m, nil
	}
	if f.Err != nil {
		return tg.ChatMember{}, f.Err
	}
	return f.Default, nil
}

// Calls returns a copy of the recorded calls.
func (f *FakeMembers) Calls() []MemberCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]MemberCall(nil), f.calls...)
}

var _ tg.MemberChecker = (*FakeMembers)(nil)
