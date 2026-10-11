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
	// UpsertUser inserts the user or updates tz, lang and calendar. It never
	// writes Username; use SetUsername.
	UpsertUser(ctx context.Context, u model.User) error
	// SetUsername stores name (lowercased, a leading "@" ignored) as the
	// user's Telegram @username, and in the same transaction clears it from
	// any other user who had it: usernames move between accounts. "" clears
	// the user's name. ErrNotFound if the user does not exist. Called on
	// the pool (not inside WithTx), concurrent calls (several nodes) giving
	// one name to different users end with exactly one owner and no error;
	// a provider whose isolation level lets them collide on the unique
	// index must retry its own transaction. Called inside WithTx it joins
	// the outer transaction, which cannot be retried, so a conflict may
	// surface as an error (and, on Postgres, abort that transaction).
	SetUsername(ctx context.Context, userID int64, name string) error
	// UserByUsername returns the user whose username is name (matched
	// case-insensitively, a leading "@" ignored). ErrNotFound if none, or
	// if name is "".
	UserByUsername(ctx context.Context, name string) (model.User, error)
	// ListNamedUsers returns every user with a known username, ordered by
	// username. The wizard's assignee step offers them (the handler
	// filters by the allowlist and drops the actor).
	ListNamedUsers(ctx context.Context) ([]model.User, error)

	// EnsureChat inserts the chat if missing, or updates its title.
	EnsureChat(ctx context.Context, c model.Chat) error
	GetChat(ctx context.Context, id int64) (model.Chat, error)
	// SetDigest sets digest_time/digest_by/digest_next_at; hhmm "" and next nil turn it off.
	SetDigest(ctx context.Context, chatID int64, hhmm string, by int64, next *time.Time) error
	// ListChatsByDigestUser returns chats whose digest was set by userID.
	ListChatsByDigestUser(ctx context.Context, userID int64) ([]model.Chat, error)
	// MigrateChat moves a chat and all its todos and reminders to newID in one transaction.
	MigrateChat(ctx context.Context, oldID, newID int64) error

	// InsertTodo assigns t.ID and the next per-chat t.Num atomically. A
	// non-zero t.AssigneeID must be an existing user.
	InsertTodo(ctx context.Context, t *model.Todo) error
	GetTodo(ctx context.Context, id int64) (model.Todo, error)
	GetTodoByNum(ctx context.Context, chatID int64, num int) (model.Todo, error)
	// ListTodos orders by (due_at IS NULL, due_at, num).
	ListTodos(ctx context.Context, q model.TodoQuery) ([]model.Todo, error)
	CountTodos(ctx context.Context, q model.TodoQuery) (int, error)
	// UpdateTodo rewrites every mutable column of t.ID, AssigneeID included
	// (AssigneeName is ignored).
	UpdateTodo(ctx context.Context, t model.Todo) error
	// ListRecurringByCreator returns open recurring todos created by userID.
	ListRecurringByCreator(ctx context.Context, userID int64) ([]model.Todo, error)
	// ListAssigned returns the open todos assigned to userID, in every chat,
	// ordered by (due_at IS NULL, due_at, chat_id, num).
	ListAssigned(ctx context.Context, userID int64) ([]model.Todo, error)

	// InsertReminder assigns r.ID.
	InsertReminder(ctx context.Context, r *model.Reminder) error
	GetReminder(ctx context.Context, id int64) (model.Reminder, error)
	// UpdateReminder overwrites the row unconditionally and clears its claim.
	// Use it for user actions (snooze, re-time); the scheduler path uses the
	// conditional RescheduleClaimed / MarkReminderSent instead.
	UpdateReminder(ctx context.Context, r model.Reminder) error
	// CancelPending cancels pending reminders of a todo; no kinds = all kinds.
	CancelPending(ctx context.Context, todoID int64, kinds ...model.ReminderKind) error
	// ClaimReminders atomically (one statement, or one transaction) claims up
	// to limit pending reminders with fire_at <= now that are unclaimed or
	// whose claim expired (claim_until <= now), stamping claim_token=token and
	// claim_until=until, and returns them oldest first with ClaimToken set.
	// token must be unique per call (providers may read rows back by token).
	// A claimed row is invisible to other claimers until claim_until.
	ClaimReminders(ctx context.Context, token string, now, until time.Time, limit int) ([]model.DueReminder, error)
	// RescheduleClaimed writes r.FireAt, r.Attempts and r.State and clears the
	// claim, but only WHERE state='pending' AND claim_token=r.ClaimToken.
	// ok=false means the claim was lost (expired and taken by another node,
	// or the reminder was already sent): the caller must not retry. Used for
	// failure backoff, permanent failure and cancelling.
	RescheduleClaimed(ctx context.Context, r model.Reminder) (ok bool, err error)
	// MarkReminderSent is the conditional pending->sent transition (sets
	// msg_id, sent_at, clears the claim). ok=false if it was no longer pending.
	MarkReminderSent(ctx context.Context, id int64, msgID int, at time.Time) (ok bool, err error)
	// NextReminderAt returns the earliest time a pending reminder becomes
	// claimable: MIN(max(fire_at, claim_until)). ok=false if none.
	NextReminderAt(ctx context.Context) (t time.Time, ok bool, err error)

	// ClaimDigests claims chats with digest_next_at <= now whose digest claim
	// is free or expired, like ClaimReminders (DigestClaimToken set; token
	// unique per call). SetDigest clears the claim.
	ClaimDigests(ctx context.Context, token string, now, until time.Time, limit int) ([]model.Chat, error)
	// AdvanceDigest sets digest_next_at=next and clears the claim, only WHERE
	// digest_claim_token=token. ok=false: the claim was lost.
	AdvanceDigest(ctx context.Context, chatID int64, token string, next time.Time) (ok bool, err error)
	// NextDigestAt is MIN(max(digest_next_at, digest_claim_until)).
	NextDigestAt(ctx context.Context) (t time.Time, ok bool, err error)

	// AcquireLease takes or renews the named lease. It succeeds if the lease
	// is missing, expired (until <= now) or held by holder, and then sets
	// holder and until=now+ttl. Epoch is incremented on every acquisition
	// that is not the renewal of a live lease by the same holder (a holder
	// re-taking its own expired lease gets a new epoch too). It returns the
	// row as it is afterwards, with a nil error also when another holder has
	// it; the caller compares Holder. holder must be unique per process
	// (lease uses NodeID+":"+random).
	AcquireLease(ctx context.Context, name, holder string, now time.Time, ttl time.Duration) (model.Lease, error)
	// ReleaseLease sets until to the Unix epoch (0) if holder holds the lease,
	// so another node can take it immediately. No-op otherwise.
	ReleaseLease(ctx context.Context, name, holder string) error

	// PutPrompt stores (upserts on ChatID+MsgID) a pending force-reply prompt.
	PutPrompt(ctx context.Context, p model.Prompt) error
	// TakePrompt deletes and returns the prompt (chatID, msgID) if it belongs
	// to userID and has not expired; exactly one concurrent caller wins.
	// Missing, another user's (row kept), expired (row left for Purge) or
	// lost to a concurrent caller -> ErrNotFound.
	TakePrompt(ctx context.Context, chatID int64, msgID int, userID int64, now time.Time) (model.Prompt, error)
	// MarkUpdate records a Telegram update_id; first=false if already seen.
	MarkUpdate(ctx context.Context, updateID int64, now time.Time) (first bool, err error)
	// Purge deletes expired prompts, expired drafts (expires_at <= now)
	// and update ids older than 48h.
	Purge(ctx context.Context, now time.Time) error

	// ---- Wizard drafts (#68, ARCHITECTURE §12) ----
	//
	// One draft per (chat, user). Version is a compare-and-set token: the
	// store sets it on every write, and UpdateDraft / DeleteDraft apply
	// only while it still matches, so two nodes handling two presses of
	// one card cannot both win. A draft whose ExpiresAt <= now is treated
	// as missing by every read and conditional write; Purge deletes it.

	// PutDraft inserts d, replacing any draft of (d.ChatID, d.UserID)
	// whatever its version or expiry (starting the wizard again). It sets
	// d.Version to a value greater than the replaced draft's, if any.
	// d.UpdatedAt and d.ExpiresAt are stored as given.
	PutDraft(ctx context.Context, d *model.Draft) error
	// GetDraft returns the draft of (chatID, userID); ErrNotFound if none
	// or expired at now.
	GetDraft(ctx context.Context, chatID, userID int64, now time.Time) (model.Draft, error)
	// DraftByMessage returns the draft whose card is (chatID, msgID),
	// whoever started it; ErrNotFound if none or expired at now.
	DraftByMessage(ctx context.Context, chatID int64, msgID int, now time.Time) (model.Draft, error)
	// UpdateDraft writes d's MsgID, AskMsgID, Step, Data, UpdatedAt and
	// ExpiresAt, only WHERE the stored version equals d.Version and the
	// stored draft has not expired at now. On success it sets d.Version to
	// the new version. ok=false means it was changed, replaced, deleted or
	// expired meanwhile: nothing was written, and exactly one of several
	// concurrent callers holding the same version gets ok=true.
	UpdateDraft(ctx context.Context, d *model.Draft, now time.Time) (ok bool, err error)
	// DeleteDraft deletes the draft of (chatID, userID) only if its version
	// is version. ok=false if it was missing or had another version; of
	// several concurrent callers exactly one gets ok=true (Save, Cancel).
	DeleteDraft(ctx context.Context, chatID, userID, version int64) (ok bool, err error)

	// WithTx runs fn in a transaction. Inside fn use ONLY tx (with SQLite the
	// pool has a single connection, so using the outer Store deadlocks).
	WithTx(ctx context.Context, fn func(tx Store) error) error
	Close() error
}
