package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// updateRetention is how long processed update ids are kept.
const updateRetention = 48 * time.Hour

// ---- reminder claims ----

// ClaimReminders claims up to limit due reminders (limit <= 0 claims none).
// The claim predicate is repeated in the outer WHERE so that databases which
// re-check only the outer predicate after a lock wait stay correct. The UPDATE stamps a per-call token; the rows are then read back by token,
// so concurrent claimers never see each other's rows.
func (d *DB) ClaimReminders(ctx context.Context, token string, now, until time.Time, limit int) ([]model.DueReminder, error) {
	if token == "" {
		return nil, errors.New("sqlstore: claim reminders: empty token")
	}
	if limit <= 0 {
		return nil, nil
	}
	sub := d.d.ClaimSubquery(`SELECT id FROM reminders
WHERE state = 'pending' AND fire_at <= ? AND claim_until <= ? ORDER BY fire_at, id LIMIT ?`)
	_, err := d.q.ExecContext(ctx,
		"UPDATE reminders SET claim_token = ?, claim_until = ? WHERE id IN ("+sub+")"+
			" AND state = 'pending' AND fire_at <= ? AND claim_until <= ?",
		token, until.Unix(), now.Unix(), now.Unix(), limit, now.Unix(), now.Unix())
	if err != nil {
		return nil, fmt.Errorf("sqlstore: claim reminders: %w", err)
	}
	rows, err := d.q.QueryContext(ctx, "SELECT "+reminderColsR+", "+todoColsT+` FROM reminders r JOIN todos t ON t.id = r.todo_id
WHERE r.claim_token = ? AND r.state = 'pending' ORDER BY r.fire_at, r.id`, token)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: read claimed reminders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.DueReminder
	for rows.Next() {
		var dr model.DueReminder
		rd, rfinish := reminderDest(&dr.Reminder)
		td, tfinish := todoDest(&dr.Todo)
		if err := rows.Scan(append(rd, td...)...); err != nil {
			return nil, fmt.Errorf("sqlstore: scan claimed reminder: %w", err)
		}
		rfinish()
		tfinish()
		out = append(out, dr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlstore: iterate claimed reminders: %w", err)
	}
	return out, nil
}

// RescheduleClaimed writes fire_at, attempts and state and clears the claim,
// only while the reminder is pending and still claimed with r.ClaimToken.
func (d *DB) RescheduleClaimed(ctx context.Context, r model.Reminder) (bool, error) {
	state := r.State
	if state == "" {
		state = model.ReminderPending
	}
	if r.ClaimToken == "" {
		return false, nil
	}
	res, err := d.q.ExecContext(ctx, `
UPDATE reminders SET fire_at = ?, attempts = ?, state = ?, claim_token = NULL, claim_until = 0
WHERE id = ? AND state = 'pending' AND claim_token = ?`,
		r.FireAt.Unix(), r.Attempts, string(state), r.ID, r.ClaimToken)
	n, err := affected(res, err, fmt.Sprintf("reschedule reminder %d", r.ID))
	return n > 0, err
}

// MarkReminderSent is the conditional pending->sent transition.
func (d *DB) MarkReminderSent(ctx context.Context, id int64, msgID int, at time.Time) (bool, error) {
	res, err := d.q.ExecContext(ctx, `
UPDATE reminders SET state = 'sent', message_id = ?, sent_at = ?, claim_token = NULL, claim_until = 0
WHERE id = ? AND state = 'pending'`, nullInt(int64(msgID)), at.Unix(), id)
	n, err := affected(res, err, fmt.Sprintf("mark reminder %d sent", id))
	return n > 0, err
}

// NextReminderAt returns MIN(max(fire_at, claim_until)) over pending
// reminders, ok=false if there are none.
func (d *DB) NextReminderAt(ctx context.Context) (time.Time, bool, error) {
	return d.minTime(ctx, `SELECT MIN(CASE WHEN fire_at >= claim_until THEN fire_at ELSE claim_until END)
FROM reminders WHERE state = 'pending'`)
}

// ---- digest claims ----

// ClaimDigests claims up to limit chats whose digest is due (limit <= 0
// claims none), like ClaimReminders.
func (d *DB) ClaimDigests(ctx context.Context, token string, now, until time.Time, limit int) ([]model.Chat, error) {
	if token == "" {
		return nil, errors.New("sqlstore: claim digests: empty token")
	}
	if limit <= 0 {
		return nil, nil
	}
	sub := d.d.ClaimSubquery(`SELECT id FROM chats
WHERE digest_next_at IS NOT NULL AND digest_next_at <= ? AND digest_claim_until <= ?
ORDER BY digest_next_at, id LIMIT ?`)
	_, err := d.q.ExecContext(ctx,
		"UPDATE chats SET digest_claim_token = ?, digest_claim_until = ? WHERE id IN ("+sub+")"+
			" AND digest_next_at IS NOT NULL AND digest_next_at <= ? AND digest_claim_until <= ?",
		token, until.Unix(), now.Unix(), now.Unix(), limit, now.Unix(), now.Unix())
	if err != nil {
		return nil, fmt.Errorf("sqlstore: claim digests: %w", err)
	}
	chats, err := d.queryChats(ctx,
		"SELECT "+chatCols+" FROM chats WHERE digest_claim_token = ? ORDER BY digest_next_at, id", token)
	if err != nil {
		return nil, err
	}
	return chats, nil
}

// AdvanceDigest sets digest_next_at and clears the claim, only while the
// chat is still claimed with token.
func (d *DB) AdvanceDigest(ctx context.Context, chatID int64, token string, next time.Time) (bool, error) {
	if token == "" {
		return false, nil
	}
	res, err := d.q.ExecContext(ctx, `
UPDATE chats SET digest_next_at = ?, digest_claim_token = NULL, digest_claim_until = 0
WHERE id = ? AND digest_claim_token = ?`, next.Unix(), chatID, token)
	n, err := affected(res, err, fmt.Sprintf("advance digest of chat %d", chatID))
	return n > 0, err
}

// NextDigestAt is MIN(max(digest_next_at, digest_claim_until)).
func (d *DB) NextDigestAt(ctx context.Context) (time.Time, bool, error) {
	return d.minTime(ctx, `SELECT MIN(CASE WHEN digest_next_at >= digest_claim_until THEN digest_next_at ELSE digest_claim_until END)
FROM chats WHERE digest_next_at IS NOT NULL`)
}

// ---- leases ----

// AcquireLease takes or renews the named lease; see store.Store. The epoch
// is bumped by the same statement that changes the holder, so it is atomic
// without a prior read. epoch is assigned before holder: MySQL evaluates SET
// assignments left to right. held_until never moves backwards: a stale
// renewal by the holder (older now, landing late) keeps the later value.
func (d *DB) AcquireLease(ctx context.Context, name, holder string, now time.Time, ttl time.Duration) (model.Lease, error) {
	var l model.Lease
	err := d.WithTx(ctx, func(tx store.Store) error {
		t := tx.(*DB)
		if _, err := t.q.ExecContext(ctx,
			d.d.InsertIgnore("leases", []string{"name", "holder", "held_until", "epoch"}, []string{"name"}),
			name, "", 0, 0); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if _, err := t.q.ExecContext(ctx, `
UPDATE leases SET
  epoch = epoch + CASE WHEN holder = ? AND held_until > ? THEN 0 ELSE 1 END,
  holder = ?, held_until = `+d.d.Greatest("held_until", "?")+`
WHERE name = ? AND (held_until <= ? OR holder = ?)`,
			holder, now.Unix(), holder, now.Add(ttl).Unix(), name, now.Unix(), holder); err != nil {
			return fmt.Errorf("update: %w", err)
		}
		var until int64
		if err := t.q.QueryRowContext(ctx,
			"SELECT name, holder, held_until, epoch FROM leases WHERE name = ?", name).
			Scan(&l.Name, &l.Holder, &until, &l.Epoch); err != nil {
			return fmt.Errorf("read: %w", err)
		}
		l.Until = fromUnix(until)
		return nil
	})
	if err != nil {
		return model.Lease{}, fmt.Errorf("sqlstore: acquire lease %q: %w", name, err)
	}
	return l, nil
}

// ReleaseLease sets held_until to 0 if holder holds the lease.
func (d *DB) ReleaseLease(ctx context.Context, name, holder string) error {
	_, err := d.q.ExecContext(ctx, "UPDATE leases SET held_until = 0 WHERE name = ? AND holder = ?", name, holder)
	if err != nil {
		return fmt.Errorf("sqlstore: release lease %q: %w", name, err)
	}
	return nil
}

// ---- prompts ----

// PutPrompt stores or replaces the prompt for (ChatID, MsgID).
func (d *DB) PutPrompt(ctx context.Context, p model.Prompt) error {
	_, err := d.q.ExecContext(ctx,
		d.d.Upsert("prompts", []string{"chat_id", "msg_id", "user_id", "kind", "todo_id", "expires_at"},
			[]string{"chat_id", "msg_id"}, []string{"user_id", "kind", "todo_id", "expires_at"}),
		p.ChatID, p.MsgID, p.UserID, string(p.Kind), p.TodoID, p.ExpiresAt.Unix())
	if err != nil {
		return fmt.Errorf("sqlstore: put prompt %d/%d: %w", p.ChatID, p.MsgID, err)
	}
	return nil
}

// TakePrompt deletes and returns the prompt if it belongs to userID and is
// not expired. The row is read and then deleted with the same conditions; the
// caller whose DELETE touches the row wins, any other gets ErrNotFound.
func (d *DB) TakePrompt(ctx context.Context, chatID int64, msgID int, userID int64, now time.Time) (model.Prompt, error) {
	var p model.Prompt
	err := d.WithTx(ctx, func(tx store.Store) error {
		t := tx.(*DB)
		var kind string
		var exp int64
		err := t.q.QueryRowContext(ctx, `
SELECT chat_id, msg_id, user_id, kind, todo_id, expires_at FROM prompts
WHERE chat_id = ? AND msg_id = ? AND user_id = ? AND expires_at > ?`,
			chatID, msgID, userID, now.Unix()).Scan(&p.ChatID, &p.MsgID, &p.UserID, &kind, &p.TodoID, &exp)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("prompt", fmt.Sprintf("%d/%d", chatID, msgID))
		}
		if err != nil {
			return err
		}
		res, err := t.q.ExecContext(ctx,
			"DELETE FROM prompts WHERE chat_id = ? AND msg_id = ? AND user_id = ? AND expires_at > ?",
			chatID, msgID, userID, now.Unix())
		n, err := affected(res, err, "delete prompt")
		if err != nil {
			return err
		}
		if n == 0 {
			return notFound("prompt", fmt.Sprintf("%d/%d", chatID, msgID))
		}
		p.Kind, p.ExpiresAt = model.PromptKind(kind), fromUnix(exp)
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.Prompt{}, err
		}
		return model.Prompt{}, fmt.Errorf("sqlstore: take prompt %d/%d: %w", chatID, msgID, err)
	}
	return p, nil
}

// ---- update dedupe and purge ----

// MarkUpdate records updateID; first=false if it was already recorded.
func (d *DB) MarkUpdate(ctx context.Context, updateID int64, now time.Time) (bool, error) {
	res, err := d.q.ExecContext(ctx,
		d.d.InsertIgnore("processed_updates", []string{"update_id", "seen_at"}, []string{"update_id"}),
		updateID, now.Unix())
	n, err := affected(res, err, fmt.Sprintf("mark update %d", updateID))
	return n > 0, err
}

// Purge deletes expired prompts and drafts, update ids older than 48h, and
// the expired web rows of purgeWeb (web.go).
func (d *DB) Purge(ctx context.Context, now time.Time) error {
	if err := d.purgeWeb(ctx, now); err != nil {
		return err
	}
	if _, err := d.q.ExecContext(ctx, "DELETE FROM prompts WHERE expires_at <= ?", now.Unix()); err != nil {
		return fmt.Errorf("sqlstore: purge prompts: %w", err)
	}
	if _, err := d.q.ExecContext(ctx, "DELETE FROM drafts WHERE expires_at <= ?", now.Unix()); err != nil {
		return fmt.Errorf("sqlstore: purge drafts: %w", err)
	}
	if _, err := d.q.ExecContext(ctx, "DELETE FROM processed_updates WHERE seen_at <= ?",
		now.Add(-updateRetention).Unix()); err != nil {
		return fmt.Errorf("sqlstore: purge updates: %w", err)
	}
	return nil
}
