// Package storetest is the conformance suite every store.Store provider must
// pass, including its concurrency cases. Call Run from the provider's tests.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Factory prepares one fresh, empty, migrated database for the test t and
// returns a function that opens a new independent handle on it (its own
// connection pool, as another node would have). Each handle is closed by the
// suite when the test ends; a test may also close one itself.
type Factory func(t *testing.T) (open func() store.Store)

var base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// Run runs the whole suite against the provider behind f.
func Run(t *testing.T, f Factory) {
	t.Helper()
	cases := []struct {
		name string
		fn   func(t *testing.T, open func() store.Store)
	}{
		{"ReopenKeepsData", testReopen},
		{"UserRoundTrip", testUserRoundTrip},
		{"ChatsAndDigest", testChatsAndDigest},
		{"TodoCRUD", testTodoCRUD},
		{"InsertTodoConcurrent", testInsertTodoConcurrent},
		{"Reminders", testReminders},
		{"WithTx", testWithTx},
		{"MigrateChatRename", testMigrateChatRename},
		{"MigrateChatTargetExists", testMigrateChatTargetExists},
		{"ClaimReminders", testClaimReminders},
		{"ClaimRemindersConcurrent", testClaimRemindersConcurrent},
		{"RescheduleClaimed", testRescheduleClaimed},
		{"StaleClaimerReminder", testStaleClaimerReminder},
		{"MarkReminderSentConcurrent", testMarkReminderSentConcurrent},
		{"NextReminderAtWithClaims", testNextReminderAt},
		{"ClaimDigests", testClaimDigests},
		{"ClaimDigestsConcurrent", testClaimDigestsConcurrent},
		{"StaleClaimerDigest", testStaleClaimerDigest},
		{"Lease", testLease},
		{"LeaseConcurrent", testLeaseConcurrent},
		{"Prompts", testPrompts},
		{"TakePromptConcurrent", testTakePromptConcurrent},
		{"MarkUpdate", testMarkUpdate},
		{"MarkUpdateConcurrent", testMarkUpdateConcurrent},
		{"Purge", testPurge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := f(t)
			open := func() store.Store {
				s := raw()
				t.Cleanup(func() { _ = s.Close() })
				return s
			}
			c.fn(t, open)
		})
	}
}

// ---- helpers ----

func chat(t *testing.T, d store.Store, id int64) {
	t.Helper()
	err := d.EnsureChat(context.Background(), model.Chat{ID: id, Kind: model.ChatGroup, Title: "c", CreatedAt: base})
	if err != nil {
		t.Fatal(err)
	}
}

func todo(t *testing.T, d store.Store, chatID int64, text string, due *time.Time) model.Todo {
	t.Helper()
	td := model.Todo{ChatID: chatID, Text: text, DueAt: due, TZ: "UTC", CreatedBy: 7, CreatedAt: base}
	if err := d.InsertTodo(context.Background(), &td); err != nil {
		t.Fatal(err)
	}
	return td
}

// tp returns base plus h hours.
func tp(h int) *time.Time { x := base.Add(time.Duration(h) * time.Hour); return &x }

// reminder inserts a pending due reminder on a fresh todo of chat 1 (created
// if missing) firing at base+fireH hours.
func reminder(t *testing.T, d store.Store, fireH int, kind model.ReminderKind) model.Reminder {
	t.Helper()
	ctx := context.Background()
	if _, err := d.GetChat(ctx, 1); err != nil {
		chat(t, d, 1)
	}
	td := todo(t, d, 1, "t", nil)
	r := model.Reminder{TodoID: td.ID, ChatID: 1, FireAt: *tp(fireH), Kind: kind, CreatedAt: base}
	if err := d.InsertReminder(ctx, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func getReminder(t *testing.T, d store.Store, id int64) model.Reminder {
	t.Helper()
	r, err := d.GetReminder(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
