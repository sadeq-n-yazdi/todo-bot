package scheduler_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
)

var _ scheduler.Backend = (*fakeBackend)(nil)

// fakeRow is a reminder with its todo and claim state.
type fakeRow struct {
	dr         model.DueReminder
	claimUntil time.Time
	done       bool // sent, cancelled or failed
}

// fakeBackend is an in-memory scheduler.Backend that implements claims, so
// several schedulers can share it.
type fakeBackend struct {
	mu  sync.Mutex
	clk clock.Clock

	claimTTL time.Duration
	rows     []*fakeRow
	nextTok  int

	chats       []model.Chat // digests, claimed when DigestNextAt <= now
	digestClaim map[int64]time.Time
	digestTok   map[int64]string
	prefsCalls  []int64
	viewPanic   bool
	views       map[int64]model.DigestView

	prefs map[int64]model.Prefs

	// recorded effects
	sent        map[int64]int // reminder id -> times ReminderSent
	failed      map[int64][]bool
	cancelled   []int64
	migrated    [][2]int64
	digestsDone []int64
	housekeeps  int
	claimCalls  int

	claimErr error
	prefsErr error
	// prefsErrFor fails Prefs for one user only.
	prefsErrFor map[int64]error
	// sentMsgID records the msgID ReminderSent was called with.
	sentMsgID  map[int64]int
	viewErr    error
	sentErrFor map[int64]error
}

func newFakeBackend(clk clock.Clock) *fakeBackend {
	return &fakeBackend{
		clk:         clk,
		claimTTL:    2 * time.Minute,
		digestClaim: map[int64]time.Time{},
		digestTok:   map[int64]string{},
		views:       map[int64]model.DigestView{},
		prefs:       map[int64]model.Prefs{},
		sent:        map[int64]int{},
		failed:      map[int64][]bool{},
		sentErrFor:  map[int64]error{},

		prefsErrFor: map[int64]error{},
		sentMsgID:   map[int64]int{},
	}
}

func (f *fakeBackend) addReminder(id int64, fireAt time.Time, status model.TodoStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, &fakeRow{dr: model.DueReminder{
		Reminder: model.Reminder{ID: id, TodoID: id, ChatID: 100 + id, FireAt: fireAt, Kind: model.ReminderDue, State: model.ReminderPending},
		Todo:     model.Todo{ID: id, ChatID: 100 + id, Text: fmt.Sprintf("todo %d", id), Status: status, CreatedBy: 7},
	}})
}

func (f *fakeBackend) row(id int64) *fakeRow {
	for _, r := range f.rows {
		if r.dr.ID == id {
			return r
		}
	}
	return nil
}

func (f *fakeBackend) sentCount(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[id]
}

func (f *fakeBackend) claimCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claimCalls
}

func (f *fakeBackend) NextWakeup(_ context.Context) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best time.Time
	ok := false
	for _, r := range f.rows {
		if r.done {
			continue
		}
		t := r.dr.FireAt
		if r.claimUntil.After(t) {
			t = r.claimUntil
		}
		if !ok || t.Before(best) {
			best, ok = t, true
		}
	}
	return best, ok, nil
}

func (f *fakeBackend) ClaimReminders(_ context.Context, now time.Time, limit int) ([]model.DueReminder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls++
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	var out []model.DueReminder
	for _, r := range f.rows {
		if len(out) >= limit {
			break
		}
		if r.done || r.dr.FireAt.After(now) || r.claimUntil.After(now) {
			continue
		}
		f.nextTok++
		r.dr.ClaimToken = fmt.Sprintf("tok%d", f.nextTok)
		r.claimUntil = now.Add(f.claimTTL)
		out = append(out, r.dr)
	}
	return out, nil
}

func (f *fakeBackend) held(r model.Reminder) *fakeRow {
	row := f.row(r.ID)
	if row == nil || row.dr.ClaimToken != r.ClaimToken || row.done {
		return nil
	}
	return row
}

func (f *fakeBackend) ReminderSent(ctx context.Context, id int64, msgID int, _ time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.sentErrFor[id]; err != nil {
		return err
	}
	f.sent[id]++
	f.sentMsgID[id] = msgID
	f.row(id).done = true
	return nil
}

func (f *fakeBackend) ReminderFailed(_ context.Context, r model.Reminder, _ time.Time, permanent bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.held(r)
	if row == nil {
		return nil
	}
	f.failed[r.ID] = append(f.failed[r.ID], permanent)
	row.claimUntil = time.Time{}
	if permanent {
		row.done = true
	} else {
		row.dr.FireAt = f.clk.Now().Add(time.Hour) // backoff
	}
	return nil
}

func (f *fakeBackend) CancelReminder(_ context.Context, r model.Reminder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row := f.held(r); row != nil {
		row.done = true
		f.cancelled = append(f.cancelled, r.ID)
	}
	return nil
}

func (f *fakeBackend) ClaimDigests(_ context.Context, now time.Time) ([]model.Chat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Chat
	for _, c := range f.chats {
		if c.DigestNextAt == nil || c.DigestNextAt.After(now) || f.digestClaim[c.ID].After(now) {
			continue
		}
		f.digestClaim[c.ID] = now.Add(f.claimTTL)
		f.nextTok++
		c.DigestClaimToken = fmt.Sprintf("dtok%d", f.nextTok)
		f.digestTok[c.ID] = c.DigestClaimToken
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeBackend) DigestDone(ctx context.Context, c model.Chat, _ time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.digestTok[c.ID] != c.DigestClaimToken || c.DigestClaimToken == "" {
		return nil // claim lost: no-op
	}
	f.digestTok[c.ID] = ""
	f.digestsDone = append(f.digestsDone, c.ID)
	for i := range f.chats {
		if f.chats[i].ID == c.ID {
			f.chats[i].DigestNextAt = nil
		}
	}
	return nil
}

func (f *fakeBackend) DigestView(_ context.Context, chatID int64, _ time.Time) (model.DigestView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.viewErr != nil {
		return model.DigestView{}, f.viewErr
	}
	if f.viewPanic {
		panic("view boom")
	}
	return f.views[chatID], nil
}

func (f *fakeBackend) MigrateChat(_ context.Context, oldID, newID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.migrated = append(f.migrated, [2]int64{oldID, newID})
	// Like the real store, the chat row (and so its digest claim) moves to
	// the new id: the claim token follows it.
	if tok, ok := f.digestTok[oldID]; ok {
		f.digestTok[newID] = tok
		delete(f.digestTok, oldID)
		f.digestClaim[newID] = f.digestClaim[oldID]
		delete(f.digestClaim, oldID)
	}
	for i := range f.chats {
		if f.chats[i].ID == oldID {
			f.chats[i].ID = newID
		}
	}
	return nil
}

func (f *fakeBackend) Prefs(_ context.Context, userID int64, _ string) (model.Prefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefsCalls = append(f.prefsCalls, userID)
	if f.prefsErr != nil {
		return model.Prefs{}, f.prefsErr
	}
	if err := f.prefsErrFor[userID]; err != nil {
		return model.Prefs{}, err
	}
	if p, ok := f.prefs[userID]; ok {
		return p, nil
	}
	return model.Prefs{TZ: time.UTC, Locale: i18n.Locale{Lang: i18n.En}}, nil
}

func (f *fakeBackend) Housekeep(_ context.Context, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.housekeeps++
	return nil
}

var errBoom = errors.New("boom")
