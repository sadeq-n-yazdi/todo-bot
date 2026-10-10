package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Assignment (#58): usernames and todos.assignee_id.

func user(t *testing.T, d store.Store, id int64) {
	t.Helper()
	u := model.User{ID: id, TZ: "UTC", Lang: i18n.En, Calendar: calendar.Gregorian, CreatedAt: base}
	if err := d.UpsertUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

func setName(t *testing.T, d store.Store, id int64, name string) {
	t.Helper()
	if err := d.SetUsername(context.Background(), id, name); err != nil {
		t.Fatalf("SetUsername(%d, %q): %v", id, name, err)
	}
}

func nameOf(t *testing.T, d store.Store, id int64) string {
	t.Helper()
	u, err := d.GetUser(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.Username
}

func testUsername(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	if err := d.SetUsername(ctx, 5, "wife"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user: want ErrNotFound, got %v", err)
	}
	user(t, d, 5)
	if got := nameOf(t, d, 5); got != "" {
		t.Fatalf("new user has username %q", got)
	}
	setName(t, d, 5, "@Wife_99")
	if got := nameOf(t, d, 5); got != "wife_99" {
		t.Fatalf("stored %q, want lowercase without @", got)
	}
	for _, q := range []string{"wife_99", "WIFE_99", "@Wife_99"} {
		u, err := d.UserByUsername(ctx, q)
		if err != nil || u.ID != 5 || u.Username != "wife_99" || u.TZ != "UTC" {
			t.Fatalf("UserByUsername(%q) = %+v, %v", q, u, err)
		}
	}
	for _, q := range []string{"", "@", "nobody"} {
		if _, err := d.UserByUsername(ctx, q); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("UserByUsername(%q): want ErrNotFound, got %v", q, err)
		}
	}
	// UpsertUser (SetLang, SetTZ, ...) never touches the username.
	u := model.User{ID: 5, TZ: "Asia/Tehran", Lang: i18n.Fa, Calendar: calendar.Jalali, CreatedAt: base}
	if err := d.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(t, d, 5); got != "wife_99" {
		t.Fatalf("UpsertUser changed username to %q", got)
	}
	// Setting the same name again is a no-op; "" clears it.
	setName(t, d, 5, "wife_99")
	setName(t, d, 5, "")
	if got := nameOf(t, d, 5); got != "" {
		t.Fatalf("not cleared: %q", got)
	}
	if _, err := d.UserByUsername(ctx, "wife_99"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cleared name still resolves: %v", err)
	}
}

// A username seen on a new account is cleared from the old one.
func testUsernameMoves(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	user(t, d, 1)
	user(t, d, 2)
	setName(t, d, 1, "alice")
	setName(t, d, 2, "Alice")
	if got := nameOf(t, d, 1); got != "" {
		t.Fatalf("old account keeps %q", got)
	}
	if got := nameOf(t, d, 2); got != "alice" {
		t.Fatalf("new account has %q", got)
	}
	u, err := d.UserByUsername(ctx, "alice")
	if err != nil || u.ID != 2 {
		t.Fatalf("resolves to %+v, %v", u, err)
	}
	// Renaming does not disturb other users.
	setName(t, d, 1, "bob")
	setName(t, d, 2, "carol")
	if nameOf(t, d, 1) != "bob" || nameOf(t, d, 2) != "carol" {
		t.Fatalf("names = %q %q", nameOf(t, d, 1), nameOf(t, d, 2))
	}
	// Inside a transaction too.
	err = d.WithTx(ctx, func(tx store.Store) error { return tx.SetUsername(ctx, 1, "carol") })
	if err != nil {
		t.Fatal(err)
	}
	if nameOf(t, d, 1) != "carol" || nameOf(t, d, 2) != "" {
		t.Fatalf("after tx: %q %q", nameOf(t, d, 1), nameOf(t, d, 2))
	}
}

// Several nodes recording the same name for different accounts at once end
// with exactly one owner.
func testUsernameConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	const n = 8
	for i := range n {
		user(t, hs[0], int64(i+1))
	}
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := hs[i%len(hs)].SetUsername(ctx, int64(i+1), "shared"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	owners := 0
	for i := range n {
		if nameOf(t, hs[0], int64(i+1)) == "shared" {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("%d owners of one username", owners)
	}
	if _, err := hs[0].UserByUsername(ctx, "shared"); err != nil {
		t.Fatal(err)
	}
}

func testAssignee(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	user(t, d, 5)
	setName(t, d, 5, "wife")

	bad := model.Todo{ChatID: 1, Text: "x", TZ: "UTC", CreatedBy: 7, AssigneeID: 999, CreatedAt: base}
	if err := d.InsertTodo(ctx, &bad); err == nil {
		t.Fatal("unknown assignee must fail")
	}

	a := model.Todo{ChatID: 1, Text: "pick up", TZ: "UTC", CreatedBy: 7, AssigneeID: 5,
		AssigneeName: "ignored", DueAt: tp(1), Recur: "daily 09:00", CreatedAt: base}
	if err := d.InsertTodo(ctx, &a); err != nil {
		t.Fatal(err)
	}
	plain := todo(t, d, 1, "plain", nil)

	check := func(what string, got model.Todo, err error) {
		t.Helper()
		if err != nil || got.ID != a.ID || got.AssigneeID != 5 || got.AssigneeName != "wife" {
			t.Fatalf("%s = %+v, %v", what, got, err)
		}
	}
	g, err := d.GetTodo(ctx, a.ID)
	check("GetTodo", g, err)
	g, err = d.GetTodoByNum(ctx, 1, a.Num)
	check("GetTodoByNum", g, err)
	list, err := d.ListTodos(ctx, model.TodoQuery{ChatID: 1})
	if err != nil || len(list) != 2 {
		t.Fatalf("ListTodos = %+v, %v", list, err)
	}
	check("ListTodos", list[0], nil)
	if list[1].AssigneeID != 0 || list[1].AssigneeName != "" {
		t.Fatalf("unassigned todo = %+v", list[1])
	}
	rec, err := d.ListRecurringByCreator(ctx, 7)
	if err != nil || len(rec) != 1 {
		t.Fatalf("ListRecurringByCreator = %+v, %v", rec, err)
	}
	check("ListRecurringByCreator", rec[0], nil)

	r := model.Reminder{TodoID: a.ID, ChatID: 1, FireAt: *tp(1), Kind: model.ReminderDue, CreatedAt: base}
	if err := d.InsertReminder(ctx, &r); err != nil {
		t.Fatal(err)
	}
	due, err := d.ClaimReminders(ctx, "tok", *tp(1), tp(1).Add(claimTTL), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("ClaimReminders = %+v, %v", due, err)
	}
	check("ClaimReminders", due[0].Todo, nil)
	if due[0].ChatID != 1 {
		t.Fatalf("reminder chat = %d, want the creating chat", due[0].ChatID)
	}

	// The name is read live: renaming the user shows up on the todo.
	setName(t, d, 5, "Spouse")
	if g, _ := d.GetTodo(ctx, a.ID); g.AssigneeName != "spouse" {
		t.Fatalf("AssigneeName = %q after rename", g.AssigneeName)
	}

	// UpdateTodo writes AssigneeID (and ignores AssigneeName).
	plain.AssigneeID, plain.AssigneeName = 5, "nope"
	if err := d.UpdateTodo(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if g, _ := d.GetTodo(ctx, plain.ID); g.AssigneeID != 5 || g.AssigneeName != "spouse" {
		t.Fatalf("after assign = %+v", g)
	}
	plain.AssigneeID = 0
	if err := d.UpdateTodo(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if g, _ := d.GetTodo(ctx, plain.ID); g.AssigneeID != 0 || g.AssigneeName != "" {
		t.Fatalf("after unassign = %+v", g)
	}

	// The assignee survives a chat migration.
	if err := d.MigrateChat(ctx, 1, -100); err != nil {
		t.Fatal(err)
	}
	if g, _ := d.GetTodo(ctx, a.ID); g.ChatID != -100 || g.AssigneeID != 5 {
		t.Fatalf("after migrate = %+v", g)
	}
}

func testListAssigned(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	chat(t, d, 2)
	user(t, d, 5)
	user(t, d, 6)
	add := func(chatID, assignee int64, text string, due int, status model.TodoStatus) model.Todo {
		t.Helper()
		td := model.Todo{ChatID: chatID, Text: text, TZ: "UTC", CreatedBy: 7, AssigneeID: assignee, CreatedAt: base}
		if due != 0 {
			td.DueAt = tp(due)
		}
		if err := d.InsertTodo(ctx, &td); err != nil {
			t.Fatal(err)
		}
		if status != model.TodoOpen {
			td.Status = status
			if err := d.UpdateTodo(ctx, td); err != nil {
				t.Fatal(err)
			}
		}
		return td
	}
	noDate2 := add(2, 5, "no date in 2", 0, model.TodoOpen)
	later := add(1, 5, "later", 5, model.TodoOpen)
	noDate1 := add(1, 5, "no date in 1", 0, model.TodoOpen)
	sooner := add(2, 5, "sooner", 2, model.TodoOpen)
	add(1, 5, "done", 1, model.TodoDone)
	add(1, 5, "deleted", 1, model.TodoDeleted)
	add(1, 6, "someone else's", 1, model.TodoOpen)
	add(1, 0, "unassigned", 1, model.TodoOpen)

	got, err := d.ListAssigned(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{sooner.ID, later.ID, noDate1.ID, noDate2.ID}
	if len(got) != len(want) {
		t.Fatalf("got %d todos: %+v", len(got), got)
	}
	for i, td := range got {
		if td.ID != want[i] || td.AssigneeID != 5 {
			t.Fatalf("order: got[%d] = %+v, want id %d", i, td, want[i])
		}
	}
	if l, err := d.ListAssigned(ctx, 99); err != nil || len(l) != 0 {
		t.Fatalf("nobody's = %+v, %v", l, err)
	}
}

// MigrateChat into an existing chat (the merge path, which renumbers
// clashing todos) keeps every todo's assignee.
func testAssigneeMigrateMerge(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	chat(t, d, -100)
	user(t, d, 5)
	setName(t, d, 5, "wife")
	todo(t, d, -100, "already there", nil) // num 1 in the target: forces a clash
	moved := model.Todo{ChatID: 1, Text: "assigned", TZ: "UTC", CreatedBy: 7, AssigneeID: 5, CreatedAt: base}
	if err := d.InsertTodo(ctx, &moved); err != nil {
		t.Fatal(err)
	}
	if err := d.MigrateChat(ctx, 1, -100); err != nil {
		t.Fatal(err)
	}
	g, err := d.GetTodo(ctx, moved.ID)
	if err != nil || g.ChatID != -100 || g.Num == 1 || g.AssigneeID != 5 || g.AssigneeName != "wife" {
		t.Fatalf("after merge = %+v, %v", g, err)
	}
	l, err := d.ListAssigned(ctx, 5)
	if err != nil || len(l) != 1 || l[0].ID != moved.ID {
		t.Fatalf("ListAssigned after merge = %+v, %v", l, err)
	}
}
