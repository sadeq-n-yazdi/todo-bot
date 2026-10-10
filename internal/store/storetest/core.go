package storetest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

func testReopen(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	if m, ok := d.(store.Migrator); ok {
		if err := m.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := m.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = open()
	if _, err := d.GetChat(ctx, 1); err != nil {
		t.Fatalf("data lost after reopen: %v", err)
	}
}

func testUserRoundTrip(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	if _, err := d.GetUser(ctx, 5); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	u := model.User{ID: 5, TZ: "Asia/Tehran", Lang: i18n.Fa, Calendar: calendar.Jalali, CreatedAt: base}
	if err := d.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetUser(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got != u {
		t.Fatalf("got %+v want %+v", got, u)
	}
	u2 := model.User{ID: 5, TZ: "Europe/Berlin", Lang: i18n.En, Calendar: calendar.Gregorian, CreatedAt: base.Add(time.Hour)}
	if err := d.UpsertUser(ctx, u2); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetUser(ctx, 5)
	u2.CreatedAt = base // created_at is kept
	if got != u2 {
		t.Fatalf("got %+v want %+v", got, u2)
	}
}

func testChatsAndDigest(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	if _, err := d.GetChat(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	chat(t, d, 1)
	if err := d.EnsureChat(ctx, model.Chat{ID: 1, Kind: model.ChatGroup, Title: "renamed"}); err != nil {
		t.Fatal(err)
	}
	c, err := d.GetChat(ctx, 1)
	if err != nil || c.Title != "renamed" || c.DigestTime != "" || c.DigestBy != 0 || c.DigestNextAt != nil || !c.CreatedAt.Equal(base) {
		t.Fatalf("chat = %+v, %v", c, err)
	}
	if err := d.SetDigest(ctx, 99, "08:00", 7, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, ok, _ := d.NextDigestAt(ctx); ok {
		t.Fatal("no digest expected")
	}
	chat(t, d, 2)
	next := base.Add(time.Hour)
	if err := d.SetDigest(ctx, 1, "08:00", 7, &next); err != nil {
		t.Fatal(err)
	}
	if err := d.SetDigest(ctx, 2, "09:00", 8, tp(5)); err != nil {
		t.Fatal(err)
	}
	c, _ = d.GetChat(ctx, 1)
	if c.DigestTime != "08:00" || c.DigestBy != 7 || c.DigestNextAt == nil || !c.DigestNextAt.Equal(next) {
		t.Fatalf("digest = %+v", c)
	}
	by7, err := d.ListChatsByDigestUser(ctx, 7)
	if err != nil || len(by7) != 1 || by7[0].ID != 1 {
		t.Fatalf("by7 = %+v, %v", by7, err)
	}
	if due, _ := d.ClaimDigests(ctx, "t0", base, base.Add(claimTTL), 10); len(due) != 0 {
		t.Fatalf("not yet due: %+v", due)
	}
	due, err := d.ClaimDigests(ctx, "t1", next, next.Add(claimTTL), 10)
	if err != nil || len(due) != 1 || due[0].ID != 1 {
		t.Fatalf("due = %+v, %v", due, err)
	}
	// Chat 1 is claimed, so it counts from its claim_until.
	n, ok, err := d.NextDigestAt(ctx)
	if err != nil || !ok || !n.Equal(next.Add(claimTTL)) || n.Location() != time.UTC {
		t.Fatalf("next = %v %v %v", n, ok, err)
	}
	if err := d.SetDigest(ctx, 1, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	c, _ = d.GetChat(ctx, 1)
	if c.DigestTime != "" || c.DigestBy != 0 || c.DigestNextAt != nil {
		t.Fatalf("digest not off: %+v", c)
	}
	if l, _ := d.ListChatsByDigestUser(ctx, 7); len(l) != 0 {
		t.Fatalf("digest off but listed: %+v", l)
	}
}

func testTodoCRUD(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	orphan := model.Todo{ChatID: 42, Text: "x", TZ: "UTC", CreatedBy: 1}
	if err := d.InsertTodo(ctx, &orphan); err == nil {
		t.Fatal("insert without chat must fail")
	}
	chat(t, d, 1)
	chat(t, d, 2)
	a := todo(t, d, 1, "a", nil)
	b := todo(t, d, 1, "b", tp(2))
	c := todo(t, d, 1, "c", tp(1))
	o := todo(t, d, 2, "other", nil)
	if a.Num != 1 || b.Num != 2 || c.Num != 3 || o.Num != 1 || a.ID == 0 || a.Status != model.TodoOpen {
		t.Fatalf("nums/ids wrong: %+v %+v %+v %+v", a, b, c, o)
	}
	got, err := d.GetTodo(ctx, b.ID)
	if err != nil || got.Text != "b" || got.DueAt == nil || !got.DueAt.Equal(*b.DueAt) || got.DoneAt != nil || got.Recur != "" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := d.GetTodo(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	byNum, err := d.GetTodoByNum(ctx, 1, 3)
	if err != nil || byNum.ID != c.ID {
		t.Fatalf("byNum = %+v, %v", byNum, err)
	}
	if _, err := d.GetTodoByNum(ctx, 1, 77); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// Order: dated first by due_at, then undated by num.
	list, err := d.ListTodos(ctx, model.TodoQuery{ChatID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if ids := []int{list[0].Num, list[1].Num, list[2].Num}; ids[0] != 3 || ids[1] != 2 || ids[2] != 1 {
		t.Fatalf("order = %v", ids)
	}
	page, _ := d.ListTodos(ctx, model.TodoQuery{ChatID: 1, Limit: 1, Offset: 1})
	if len(page) != 1 || page[0].Num != 2 {
		t.Fatalf("page = %+v", page)
	}
	before, _ := d.ListTodos(ctx, model.TodoQuery{ChatID: 1, DueBefore: tp(2)})
	if len(before) != 1 || before[0].Num != 3 {
		t.Fatalf("before = %+v", before)
	}
	if n, err := d.CountTodos(ctx, model.TodoQuery{ChatID: 1}); err != nil || n != 3 {
		t.Fatalf("count = %d, %v", n, err)
	}

	// Update: mark done, set recur.
	done := base.Add(5 * time.Hour)
	b.Status, b.DoneAt, b.Recur, b.Text, b.UpdatedAt = model.TodoDone, &done, "daily", "b2", done
	if err := d.UpdateTodo(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetTodo(ctx, b.ID)
	if got.Status != model.TodoDone || got.DoneAt == nil || !got.DoneAt.Equal(done) || got.Recur != "daily" || got.Text != "b2" || !got.UpdatedAt.Equal(done) {
		t.Fatalf("updated = %+v", got)
	}
	if n, _ := d.CountTodos(ctx, model.TodoQuery{ChatID: 1, Statuses: []model.TodoStatus{model.TodoOpen}}); n != 2 {
		t.Fatalf("open count = %d", n)
	}
	if err := d.UpdateTodo(ctx, model.Todo{ID: 9999, Status: model.TodoOpen}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// Recurring: only open ones.
	c.Recur = "weekly"
	c.UpdatedAt = base
	if err := d.UpdateTodo(ctx, c); err != nil {
		t.Fatal(err)
	}
	rec, err := d.ListRecurringByCreator(ctx, 7)
	if err != nil || len(rec) != 1 || rec[0].ID != c.ID {
		t.Fatalf("recurring = %+v, %v", rec, err)
	}
}

// handles opens n independent handles on the database.
func handles(open func() store.Store, n int) []store.Store {
	hs := make([]store.Store, n)
	for i := range hs {
		hs[i] = open()
	}
	return hs
}

// Per-chat numbers stay contiguous and unique when several handles insert at once.
func testInsertTodoConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	chat(t, hs[0], 1)
	const n = 24
	nums := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			td := model.Todo{ChatID: 1, Text: "x", TZ: "UTC", CreatedBy: 1, CreatedAt: base}
			if err := hs[i%len(hs)].InsertTodo(ctx, &td); err != nil {
				t.Error(err)
			}
			nums[i] = td.Num
		}()
	}
	wg.Wait()
	sort.Ints(nums)
	for i, v := range nums {
		if v != i+1 {
			t.Fatalf("nums not contiguous: %v", nums)
		}
	}
}

func testReminders(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	td := todo(t, d, 1, "a", tp(1))
	r1 := model.Reminder{TodoID: td.ID, ChatID: 1, FireAt: *tp(2), Kind: model.ReminderDue, CreatedAt: base}
	if err := d.InsertReminder(ctx, &r1); err != nil {
		t.Fatal(err)
	}
	if r1.ID == 0 || r1.State != model.ReminderPending {
		t.Fatalf("r1 = %+v", r1)
	}
	dup := model.Reminder{TodoID: td.ID, ChatID: 1, FireAt: *tp(3), Kind: model.ReminderDue, CreatedAt: base}
	if err := d.InsertReminder(ctx, &dup); err == nil {
		t.Fatal("second pending due reminder must be rejected")
	}
	r2 := model.Reminder{TodoID: td.ID, ChatID: 1, FireAt: *tp(1), Kind: model.ReminderCustom, CreatedAt: base}
	r3 := model.Reminder{TodoID: td.ID, ChatID: 1, FireAt: *tp(9), Kind: model.ReminderSnooze, CreatedAt: base}
	for _, r := range []*model.Reminder{&r2, &r3} {
		if err := d.InsertReminder(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.GetReminder(ctx, r1.ID)
	if err != nil || got != r1 {
		t.Fatalf("get = %+v, %v (want %+v)", got, err, r1)
	}
	if _, err := d.GetReminder(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	next, ok, err := d.NextReminderAt(ctx)
	if err != nil || !ok || !next.Equal(*tp(1)) {
		t.Fatalf("next = %v %v %v", next, ok, err)
	}
	due, err := d.ClaimReminders(ctx, "t1", *tp(2), tp(2).Add(claimTTL), 10)
	if err != nil || len(due) != 2 || due[0].ID != r2.ID || due[1].ID != r1.ID || due[0].Todo.ID != td.ID || due[0].Todo.Text != "a" {
		t.Fatalf("due = %+v, %v", due, err)
	}
	// Both are claimed until tp(2)+ttl; once that passes, two are claimable and limit 1 takes one.
	retake := tp(2).Add(claimTTL)
	if lim, _ := d.ClaimReminders(ctx, "t2", retake, retake.Add(claimTTL), 1); len(lim) != 1 {
		t.Fatalf("limit ignored: %d", len(lim))
	}

	// Update: mark sent with message id.
	sent := *tp(2)
	r1.State, r1.SentAt, r1.MessageID, r1.Attempts = model.ReminderSent, &sent, 555, 1
	if err := d.UpdateReminder(ctx, r1); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetReminder(ctx, r1.ID)
	if got.State != model.ReminderSent || got.MessageID != 555 || got.Attempts != 1 || got.SentAt == nil || !got.SentAt.Equal(sent) {
		t.Fatalf("updated = %+v", got)
	}
	if err := d.UpdateReminder(ctx, model.Reminder{ID: 999, Kind: model.ReminderDue, State: model.ReminderSent}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// A new due reminder is allowed once the first is no longer pending.
	again := model.Reminder{TodoID: td.ID, ChatID: 1, FireAt: *tp(4), Kind: model.ReminderDue, CreatedAt: base}
	if err := d.InsertReminder(ctx, &again); err != nil {
		t.Fatal(err)
	}

	// CancelPending by kind, then all.
	if err := d.CancelPending(ctx, td.ID, model.ReminderCustom); err != nil {
		t.Fatal(err)
	}
	if g, _ := d.GetReminder(ctx, r2.ID); g.State != model.ReminderCancelled {
		t.Fatalf("custom not cancelled: %+v", g)
	}
	if g, _ := d.GetReminder(ctx, again.ID); g.State != model.ReminderPending {
		t.Fatalf("due wrongly cancelled: %+v", g)
	}
	if err := d.CancelPending(ctx, td.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.NextReminderAt(ctx); ok {
		t.Fatal("no pending reminders expected")
	}
	if g, _ := d.GetReminder(ctx, r1.ID); g.State != model.ReminderSent {
		t.Fatalf("sent reminder touched: %+v", g)
	}
}

func testWithTx(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	boom := errors.New("boom")
	err := d.WithTx(ctx, func(tx store.Store) error {
		chat(t, tx, 1)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if _, err := d.GetChat(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("not rolled back: %v", err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic swallowed")
			}
		}()
		_ = d.WithTx(ctx, func(tx store.Store) error {
			chat(t, tx, 2)
			panic("x")
		})
	}()
	if _, err := d.GetChat(ctx, 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("not rolled back after panic: %v", err)
	}

	err = d.WithTx(ctx, func(tx store.Store) error {
		chat(t, tx, 3)
		return tx.WithTx(ctx, func(inner store.Store) error {
			chat(t, inner, 4)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{3, 4} {
		if _, err := d.GetChat(ctx, id); err != nil {
			t.Fatalf("chat %d not committed: %v", id, err)
		}
	}
	// The pool must be usable again after all of the above.
	chat(t, d, 5)
}

func testMigrateChatRename(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	a := todo(t, d, 1, "a", nil)
	r := model.Reminder{TodoID: a.ID, ChatID: 1, FireAt: *tp(1), Kind: model.ReminderDue, CreatedAt: base}
	if err := d.InsertReminder(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateChat(ctx, 1, -100); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetChat(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old chat remains: %v", err)
	}
	got, err := d.GetTodoByNum(ctx, -100, 1)
	if err != nil || got.ID != a.ID {
		t.Fatalf("todo = %+v, %v", got, err)
	}
	if rr, _ := d.GetReminder(ctx, r.ID); rr.ChatID != -100 {
		t.Fatalf("reminder chat = %d", rr.ChatID)
	}
	if err := d.MigrateChat(ctx, 12345, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func testMigrateChatTargetExists(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	chat(t, d, -100)
	o1 := todo(t, d, 1, "old1", nil)
	o2 := todo(t, d, 1, "old2", nil)
	n1 := todo(t, d, -100, "new1", nil)
	r := model.Reminder{TodoID: o2.ID, ChatID: 1, FireAt: *tp(1), Kind: model.ReminderDue, CreatedAt: base}
	if err := d.InsertReminder(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateChat(ctx, 1, -100); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetChat(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old chat remains: %v", err)
	}
	list, err := d.ListTodos(ctx, model.TodoQuery{ChatID: -100})
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	seen := map[int]bool{}
	for _, td := range list {
		if seen[td.Num] {
			t.Fatalf("duplicate num %d", td.Num)
		}
		seen[td.Num] = true
	}
	if g, _ := d.GetTodo(ctx, n1.ID); g.Num != 1 {
		t.Fatalf("target todo renumbered: %+v", g)
	}
	if g, _ := d.GetTodo(ctx, o1.ID); g.ChatID != -100 || g.Num == 1 {
		t.Fatalf("clashing todo not renumbered: %+v", g)
	}
	if g, _ := d.GetTodo(ctx, o2.ID); g.ChatID != -100 || g.Num == 1 {
		t.Fatalf("clashing todo not renumbered: %+v", g)
	}
	if rr, _ := d.GetReminder(ctx, r.ID); rr.ChatID != -100 {
		t.Fatalf("reminder chat = %d", rr.ChatID)
	}
	// New todos must not collide afterwards.
	fresh := todo(t, d, -100, "fresh", nil)
	if seen[fresh.Num] {
		t.Fatalf("fresh num %d already used", fresh.Num)
	}
}
