package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Wizard drafts (#68, ARCHITECTURE §12). The collected answers are stored
// as a JSON object, so new fields need no migration.

const draftCols = "chat_id, user_id, msg_id, ask_msg_id, step, data, version, updated_at, expires_at"

func scanDraft(s scanner) (model.Draft, error) {
	var d model.Draft
	var step, data string
	var upd, exp int64
	if err := s.Scan(&d.ChatID, &d.UserID, &d.MsgID, &d.AskMsgID, &step, &data, &d.Version, &upd, &exp); err != nil {
		return model.Draft{}, err
	}
	if err := json.Unmarshal([]byte(data), &d.Data); err != nil {
		return model.Draft{}, fmt.Errorf("decode draft data: %w", err)
	}
	d.Step, d.UpdatedAt, d.ExpiresAt = model.DraftStep(step), fromUnix(upd), fromUnix(exp)
	return d, nil
}

func encodeDraftData(v model.DraftData) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("sqlstore: encode draft data: %w", err)
	}
	return string(b), nil
}

// PutDraft inserts or replaces the draft of (ChatID, UserID) with a version
// greater than the replaced one's. The replacement is an in-place UPDATE
// that increments the stored version (it takes the row lock on databases
// that run read committed, so a concurrent UpdateDraft cannot make two
// drafts share a version); only a missing row is inserted, with its version
// seeded from dr.UpdatedAt in Unix milliseconds (draftSeed), so a draft
// started after a delete or a purge does not reuse an earlier version.
func (d *DB) PutDraft(ctx context.Context, dr *model.Draft) error {
	data, err := encodeDraftData(dr.Data)
	if err != nil {
		return err
	}
	upd := unixOrNow(dr.UpdatedAt)
	seed := draftSeed(dr.UpdatedAt)
	var ver int64
	err = d.WithTx(ctx, func(tx store.Store) error {
		q := tx.(*DB).q
		replace := func() (int64, error) {
			res, err := q.ExecContext(ctx, `
UPDATE drafts SET msg_id = ?, ask_msg_id = ?, step = ?, data = ?, version = version + 1,
  updated_at = ?, expires_at = ?
WHERE chat_id = ? AND user_id = ?`,
				dr.MsgID, dr.AskMsgID, string(dr.Step), data, upd, dr.ExpiresAt.Unix(), dr.ChatID, dr.UserID)
			return affected(res, err, "replace draft")
		}
		n, err := replace()
		if err != nil {
			return err
		}
		if n == 0 {
			res, err := q.ExecContext(ctx,
				d.d.InsertIgnore("drafts",
					[]string{"chat_id", "user_id", "msg_id", "ask_msg_id", "step", "data", "version", "updated_at", "expires_at"},
					[]string{"chat_id", "user_id"}),
				dr.ChatID, dr.UserID, dr.MsgID, dr.AskMsgID, string(dr.Step), data, seed, upd, dr.ExpiresAt.Unix())
			if n, err = affected(res, err, "insert draft"); err != nil {
				return err
			}
			if n == 0 { // a concurrent PutDraft inserted it first
				if _, err := replace(); err != nil {
					return err
				}
			}
		}
		return q.QueryRowContext(ctx, "SELECT version FROM drafts WHERE chat_id = ? AND user_id = ?",
			dr.ChatID, dr.UserID).Scan(&ver)
	})
	if err != nil {
		return fmt.Errorf("sqlstore: put draft %d/%d: %w", dr.ChatID, dr.UserID, err)
	}
	dr.Version = ver
	return nil
}

// draftSeed is the version of a newly inserted draft: t (now, if zero) in
// Unix milliseconds, at least 1. A draft's version then grows by one per
// write, and nobody presses a thousand buttons a second.
func draftSeed(t time.Time) int64 {
	if t.IsZero() {
		t = time.Now()
	}
	return max(t.UnixMilli(), 1)
}

func (d *DB) getDraft(ctx context.Context, key string, query string, args ...any) (model.Draft, error) {
	dr, err := scanDraft(d.q.QueryRowContext(ctx, "SELECT "+draftCols+" FROM drafts WHERE "+query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Draft{}, notFound("draft", key)
	}
	if err != nil {
		return model.Draft{}, fmt.Errorf("sqlstore: get draft %s: %w", key, err)
	}
	return dr, nil
}

// GetDraft returns the live draft of (chatID, userID).
func (d *DB) GetDraft(ctx context.Context, chatID, userID int64, now time.Time) (model.Draft, error) {
	return d.getDraft(ctx, fmt.Sprintf("%d/user %d", chatID, userID),
		"chat_id = ? AND user_id = ? AND expires_at > ?", chatID, userID, now.Unix())
}

// DraftByMessage returns the live draft whose card is (chatID, msgID).
func (d *DB) DraftByMessage(ctx context.Context, chatID int64, msgID int, now time.Time) (model.Draft, error) {
	return d.getDraft(ctx, fmt.Sprintf("%d/msg %d", chatID, msgID),
		"chat_id = ? AND msg_id = ? AND msg_id <> 0 AND expires_at > ?", chatID, msgID, now.Unix())
}

// UpdateDraft is the conditional write: it applies only while the stored
// version is dr.Version and the draft is live, and bumps the version.
func (d *DB) UpdateDraft(ctx context.Context, dr *model.Draft, now time.Time) (bool, error) {
	data, err := encodeDraftData(dr.Data)
	if err != nil {
		return false, err
	}
	res, err := d.q.ExecContext(ctx, `
UPDATE drafts SET msg_id = ?, ask_msg_id = ?, step = ?, data = ?, version = version + 1,
  updated_at = ?, expires_at = ?
WHERE chat_id = ? AND user_id = ? AND version = ? AND expires_at > ?`,
		dr.MsgID, dr.AskMsgID, string(dr.Step), data, unixOrNow(dr.UpdatedAt), dr.ExpiresAt.Unix(),
		dr.ChatID, dr.UserID, dr.Version, now.Unix())
	n, err := affected(res, err, fmt.Sprintf("update draft %d/%d", dr.ChatID, dr.UserID))
	if err != nil || n == 0 {
		return false, err
	}
	dr.Version++
	return true, nil
}

// DeleteDraft deletes the draft only if it still has version and is live.
func (d *DB) DeleteDraft(ctx context.Context, chatID, userID, version int64, now time.Time) (bool, error) {
	res, err := d.q.ExecContext(ctx,
		"DELETE FROM drafts WHERE chat_id = ? AND user_id = ? AND version = ? AND expires_at > ?",
		chatID, userID, version, now.Unix())
	n, err := affected(res, err, fmt.Sprintf("delete draft %d/%d", chatID, userID))
	return n > 0, err
}
