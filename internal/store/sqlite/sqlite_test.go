package sqlite_test

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite"
	"github.com/sadeq-n-yazdi/todo-bot/migrations"
)

// shipped returns the versions of the embedded SQLite migrations, in order.
func shipped(t *testing.T) []int {
	t.Helper()
	mfs, err := migrations.For("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, e := range entries {
		v, err := strconv.Atoi(e.Name()[:4])
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		t.Fatal("no migrations embedded")
	}
	return out
}

func open(t *testing.T) *sqlite.DB {
	t.Helper()
	d, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	if err := d.EnsureChat(ctx, model.Chat{ID: 1, Kind: model.ChatGroup, Title: "c", CreatedAt: time.Unix(1e9, 0)}); err != nil {
		t.Fatal(err)
	}
	td := model.Todo{ChatID: 1, Text: "a", TZ: "UTC", CreatedBy: 1}
	if err := d.InsertTodo(ctx, &td); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "backup.db")
	if err := d.Backup(ctx, dst); err != nil {
		t.Fatal(err)
	}
	b, err := sqlite.Open(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.GetTodoByNum(ctx, 1, 1); err != nil {
		t.Fatalf("backup lacks data: %v", err)
	}
}

func rawPragma(t *testing.T, path, pragma string) string {
	t.Helper()
	// Open through the package so the DSN is the real one, then read the
	// pragma over a second, raw handle with the same settings.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow("PRAGMA " + pragma).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDSNPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	d, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := rawPragma(t, path, "journal_mode"); got != "wal" {
		t.Fatalf("journal_mode = %q", got)
	}
}

// With _txlock=immediate a transaction takes the write lock at BEGIN, so a
// second handle waits for it instead of failing with SQLITE_BUSY when a read
// transaction tries to write.
func TestTxLockImmediate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	a, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.WithTx(ctx, func(tx store.Store) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	got := make(chan time.Time, 1)
	go func() {
		_ = b.WithTx(ctx, func(tx store.Store) error { got <- time.Now(); return nil })
	}()
	select {
	case <-got:
		t.Fatal("second transaction began while the first held the write lock")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("second transaction never ran")
	}
}

func versions(t *testing.T, path string) []int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func TestMigrateFreshRecordsVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.db")
	d, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	if got := versions(t, path); !slices.Equal(got, shipped(t)) {
		t.Fatalf("versions = %v", got)
	}
}

// A database created before schema_migrations (tracked by user_version = 1)
// is seeded, keeps its data, and only gets the newer migrations.
func TestMigrateSeedsFromUserVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	mfs, err := migrations.For("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	body, err := fs.ReadFile(mfs, "0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("INSERT INTO chats (id, kind, title, created_at) VALUES (1, 'group', 'old', 5)"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	d, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatalf("migrating an existing database: %v", err)
	}
	c, err := d.GetChat(ctx, 1)
	if err != nil || c.Title != "old" {
		t.Fatalf("data lost: %+v, %v", c, err)
	}
	_ = d.Close()
	if got := versions(t, path); !slices.Equal(got, shipped(t)) {
		t.Fatalf("versions = %v", got)
	}
}

func TestMigrateConcurrentOpen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.db")
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := sqlite.Open(ctx, path)
			if err != nil {
				t.Error(err)
				return
			}
			_ = d.Close()
		}()
	}
	wg.Wait()
	if got := versions(t, path); !slices.Equal(got, shipped(t)) {
		t.Fatalf("versions = %v", got)
	}
}

func TestOpenByURL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Chdir(dir)
	abs := filepath.Join(dir, "abs.db")
	if err := os.MkdirAll("data", 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw  string
		want string // file that must exist afterwards; "" = must fail
	}{
		{"sqlite:todo.db", "todo.db"},
		{"sqlite:data/todo.db", "data/todo.db"},
		{(&url.URL{Scheme: "sqlite", Path: abs}).String(), abs},
		{"sqlite://todo.db", ""},
		{"sqlite://u:secret@host/x.db", ""},
		{"sqlite:", ""},
		{"sqlite://", ""},
		{"sqlite://u:secret@/abs/x.db", ""},
		{"sqlite:todo.db?mode=ro", ""},
		{"sqlite:todo.db#frag", ""},
		{"sqlite:todo.db?", ""},
		{"sqlite:///nonexistent-dir/sub/x.db", ""},
	}
	for _, c := range cases {
		s, err := store.Open(ctx, c.raw)
		if c.want == "" {
			if err == nil {
				_ = s.Close()
				t.Errorf("%q: want an error", c.raw)
				continue
			}
			if s != nil { // a typed nil *DB inside the interface would also land here
				t.Errorf("%q: store.Store must be nil on error, got %T", c.raw, s)
			}
			if (len(c.raw) > 10 && strings.Contains(err.Error(), c.raw)) || strings.Contains(err.Error(), "secret") {
				t.Errorf("%q: error contains the URL: %v", c.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
		_ = s.Close()
		if _, err := os.Stat(c.want); err != nil {
			t.Errorf("%q: %v", c.raw, err)
		}
	}
}

func TestUserVersionFollowsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uv.db")
	d, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var v int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if want := shipped(t); v != want[len(want)-1] {
		t.Fatalf("user_version = %d, want %d", v, want[len(want)-1])
	}
}
