// Package store defines the persistence contract and the provider registry.
// Providers (internal/store/sqlite; postgres and mysql in v0.2) register an
// Opener for their URL scheme in init(). All times are passed and returned in
// UTC. Several processes may share one database (multi-node): row claims,
// the lease, MarkUpdate and TakePrompt are the coordination primitives.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// ErrNotFound is returned (possibly wrapped) when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store is the persistence API.
type Store interface {
	GetUser(ctx context.Context, id int64) (model.User, error)
	UpsertUser(ctx context.Context, u model.User) error

	// EnsureChat inserts the chat if missing, or updates its title.
	EnsureChat(ctx context.Context, c model.Chat) error
	GetChat(ctx context.Context, id int64) (model.Chat, error)
	// SetDigest sets digest_time/digest_by/digest_next_at; hhmm "" and next nil turn it off.
	SetDigest(ctx context.Context, chatID int64, hhmm string, by int64, next *time.Time) error
	// ListChatsByDigestUser returns chats whose digest was set by userID.
	ListChatsByDigestUser(ctx context.Context, userID int64) ([]model.Chat, error)
	// MigrateChat moves a chat and all its todos and reminders to newID in one transaction.
	MigrateChat(ctx context.Context, oldID, newID int64) error

	// InsertTodo assigns t.ID and the next per-chat t.Num atomically.
	InsertTodo(ctx context.Context, t *model.Todo) error
	GetTodo(ctx context.Context, id int64) (model.Todo, error)
	GetTodoByNum(ctx context.Context, chatID int64, num int) (model.Todo, error)
	// ListTodos orders by (due_at IS NULL, due_at, num).
	ListTodos(ctx context.Context, q model.TodoQuery) ([]model.Todo, error)
	CountTodos(ctx context.Context, q model.TodoQuery) (int, error)
	UpdateTodo(ctx context.Context, t model.Todo) error
	// ListRecurringByCreator returns open recurring todos created by userID.
	ListRecurringByCreator(ctx context.Context, userID int64) ([]model.Todo, error)

	// InsertReminder assigns r.ID.
	InsertReminder(ctx context.Context, r *model.Reminder) error
	GetReminder(ctx context.Context, id int64) (model.Reminder, error)
	UpdateReminder(ctx context.Context, r model.Reminder) error
	// CancelPending cancels pending reminders of a todo; no kinds = all kinds.
	CancelPending(ctx context.Context, todoID int64, kinds ...model.ReminderKind) error
	// UpdateReminder also clears the reminder's claim.
	//
	// ClaimReminders atomically claims up to limit pending reminders with
	// fire_at <= now that are unclaimed or whose claim expired (claim_until
	// <= now), stamping claim_token=token and claim_until=until. Oldest first.
	// A claimed row is invisible to other claimers until claim_until.
	ClaimReminders(ctx context.Context, token string, now, until time.Time, limit int) ([]model.DueReminder, error)
	// MarkReminderSent is the conditional pending->sent transition (sets
	// msg_id, sent_at, clears the claim). ok=false if it was no longer pending.
	MarkReminderSent(ctx context.Context, id int64, msgID int, at time.Time) (ok bool, err error)
	// NextReminderAt returns the earliest time a pending reminder becomes
	// claimable: MIN(max(fire_at, claim_until)). ok=false if none.
	NextReminderAt(ctx context.Context) (t time.Time, ok bool, err error)

	// ClaimDigests claims chats with digest_next_at <= now whose digest claim
	// is free or expired, like ClaimReminders. SetDigest clears the claim.
	ClaimDigests(ctx context.Context, token string, now, until time.Time, limit int) ([]model.Chat, error)
	// NextDigestAt is MIN(max(digest_next_at, digest_claim_until)).
	NextDigestAt(ctx context.Context) (t time.Time, ok bool, err error)

	// AcquireLease takes or renews the named lease for holder if it is free,
	// expired (until <= now) or already held by holder, setting until=now+ttl
	// and bumping Epoch when the holder changes. It returns the lease row as it
	// is afterwards; the caller compares Holder to learn whether it won.
	AcquireLease(ctx context.Context, name, holder string, now time.Time, ttl time.Duration) (model.Lease, error)
	// ReleaseLease expires the lease immediately if holder holds it.
	ReleaseLease(ctx context.Context, name, holder string) error

	// PutPrompt stores (upserts on ChatID+MsgID) a pending force-reply prompt.
	PutPrompt(ctx context.Context, p model.Prompt) error
	// TakePrompt removes and returns the prompt; exactly one concurrent caller
	// wins. Missing, expired (ExpiresAt <= now) or lost -> ErrNotFound.
	TakePrompt(ctx context.Context, chatID int64, msgID int, now time.Time) (model.Prompt, error)
	// MarkUpdate records a Telegram update_id; first=false if already seen.
	MarkUpdate(ctx context.Context, updateID int64, now time.Time) (first bool, err error)
	// Purge deletes expired prompts and update ids older than 48h.
	Purge(ctx context.Context, now time.Time) error

	// WithTx runs fn in a transaction. Inside fn use ONLY tx (with SQLite the
	// pool has a single connection, so using the outer Store deadlocks).
	WithTx(ctx context.Context, fn func(tx Store) error) error
	Close() error
}
