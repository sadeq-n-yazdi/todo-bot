package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Web UI (#102, docs/WEB-DESIGN.md §6). The store sees only hashes of
// tokens, codes and proofs.

// memberRetention is how long a chat_members row that is not 'member' stays
// after its last hint and its last check (§6.4).
const memberRetention = 30 * 24 * time.Hour

// unixOrZero stores the zero time as 0, so that "never" survives a round trip.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// fromUnixOrZero is the inverse of unixOrZero.
func fromUnixOrZero(x int64) time.Time {
	if x == 0 {
		return time.Time{}
	}
	return fromUnix(x)
}

// ---- sessions ----

const webSessionCols = "id, user_id, kind, device, created_at, seen_at, expires_at, max_at"

// PutWebSession inserts s.
func (d *DB) PutWebSession(ctx context.Context, s model.WebSession) error {
	_, err := d.q.ExecContext(ctx, "INSERT INTO web_sessions ("+webSessionCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		s.ID, s.UserID, string(s.Kind), s.Device, s.CreatedAt.Unix(), s.SeenAt.Unix(), s.ExpiresAt.Unix(), s.MaxAt.Unix())
	if err != nil {
		return fmt.Errorf("sqlstore: put web session of user %d: %w", s.UserID, err)
	}
	return nil
}

// GetWebSession returns the session id if it is live at now.
func (d *DB) GetWebSession(ctx context.Context, id string, now time.Time) (model.WebSession, error) {
	var s model.WebSession
	var kind string
	var created, seen, exp, maxAt int64
	err := d.q.QueryRowContext(ctx, "SELECT "+webSessionCols+" FROM web_sessions WHERE id = ? AND expires_at > ? AND max_at > ?",
		id, now.Unix(), now.Unix()).Scan(&s.ID, &s.UserID, &kind, &s.Device, &created, &seen, &exp, &maxAt)
	if errors.Is(err, sql.ErrNoRows) {
		// The id is a hash, but keep it out of errors and logs anyway.
		return model.WebSession{}, notFound("web session", "")
	}
	if err != nil {
		return model.WebSession{}, fmt.Errorf("sqlstore: get web session: %w", err)
	}
	s.Kind = model.WebSessionKind(kind)
	s.CreatedAt, s.SeenAt, s.ExpiresAt, s.MaxAt = fromUnix(created), fromUnix(seen), fromUnix(exp), fromUnix(maxAt)
	return s, nil
}

// TouchWebSession writes seen_at and expires_at; ErrNotFound if no row.
func (d *DB) TouchWebSession(ctx context.Context, id string, seenAt, expiresAt time.Time) error {
	res, err := d.q.ExecContext(ctx, "UPDATE web_sessions SET seen_at = ?, expires_at = ? WHERE id = ?",
		seenAt.Unix(), expiresAt.Unix(), id)
	return checkAffected(res, err, "touch web session", "web session", "")
}

// DeleteWebSession deletes one session.
func (d *DB) DeleteWebSession(ctx context.Context, id string) error {
	if _, err := d.q.ExecContext(ctx, "DELETE FROM web_sessions WHERE id = ?", id); err != nil {
		return fmt.Errorf("sqlstore: delete web session: %w", err)
	}
	return nil
}

// DeleteUserWebSessions deletes every session of the user.
func (d *DB) DeleteUserWebSessions(ctx context.Context, userID int64) error {
	if _, err := d.q.ExecContext(ctx, "DELETE FROM web_sessions WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("sqlstore: delete web sessions of user %d: %w", userID, err)
	}
	return nil
}

// TrimUserWebSessions keeps the newest keep sessions of one kind of the user.
func (d *DB) TrimUserWebSessions(ctx context.Context, userID int64, kind model.WebSessionKind, keep int) error {
	var err error
	if keep <= 0 {
		_, err = d.q.ExecContext(ctx, "DELETE FROM web_sessions WHERE user_id = ? AND kind = ?", userID, string(kind))
	} else {
		sub := d.d.ClaimSubquery(`SELECT id FROM web_sessions WHERE user_id = ? AND kind = ?
ORDER BY created_at DESC, id DESC LIMIT ?`)
		_, err = d.q.ExecContext(ctx,
			"DELETE FROM web_sessions WHERE user_id = ? AND kind = ? AND id NOT IN ("+sub+")",
			userID, string(kind), userID, string(kind), keep)
	}
	if err != nil {
		return fmt.Errorf("sqlstore: trim %s sessions of user %d: %w", kind, userID, err)
	}
	return nil
}

// DeleteWebSessionsExcept deletes the sessions of every user not in keep.
func (d *DB) DeleteWebSessionsExcept(ctx context.Context, keep []int64) (int, error) {
	query := "DELETE FROM web_sessions"
	args := make([]any, len(keep))
	if len(keep) > 0 {
		query += " WHERE user_id NOT IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(keep)), ", ") + ")"
		for i, id := range keep {
			args[i] = id
		}
	}
	res, err := d.q.ExecContext(ctx, query, args...)
	n, err := affected(res, err, "delete web sessions of removed users")
	return int(n), err
}

// ---- login codes ----

// errCodeCap rolls back PutLoginCode when the cap is reached.
var errCodeCap = errors.New("login code cap")

// PutLoginCode replaces the codes of c.UserID with c, unless maxLive codes
// of other users are live at now. Then it changes nothing.
func (d *DB) PutLoginCode(ctx context.Context, c model.LoginCode, maxLive int, now time.Time) (bool, error) {
	err := d.WithTx(ctx, func(tx store.Store) error {
		q := tx.(*DB).q
		if _, err := q.ExecContext(ctx, "DELETE FROM web_login_codes WHERE user_id = ?", c.UserID); err != nil {
			return fmt.Errorf("delete old codes: %w", err)
		}
		var live int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM web_login_codes WHERE expires_at > ?",
			now.Unix()).Scan(&live); err != nil {
			return fmt.Errorf("count live codes: %w", err)
		}
		if live >= maxLive {
			return errCodeCap
		}
		if _, err := q.ExecContext(ctx,
			"INSERT INTO web_login_codes (code_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
			c.CodeHash, c.UserID, c.CreatedAt.Unix(), c.ExpiresAt.Unix()); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		return nil
	})
	if errors.Is(err, errCodeCap) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("sqlstore: put login code of user %d: %w", c.UserID, err)
	}
	return true, nil
}

// TakeLoginCode reads and deletes the live code in one transaction. The
// caller whose DELETE touches the row wins; any other gets ErrNotFound.
func (d *DB) TakeLoginCode(ctx context.Context, codeHash string, now time.Time) (model.LoginCode, error) {
	var c model.LoginCode
	err := d.WithTx(ctx, func(tx store.Store) error {
		q := tx.(*DB).q
		var created, exp int64
		err := q.QueryRowContext(ctx,
			"SELECT code_hash, user_id, created_at, expires_at FROM web_login_codes WHERE code_hash = ? AND expires_at > ?",
			codeHash, now.Unix()).Scan(&c.CodeHash, &c.UserID, &created, &exp)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("login code", "")
		}
		if err != nil {
			return err
		}
		res, err := q.ExecContext(ctx, "DELETE FROM web_login_codes WHERE code_hash = ? AND expires_at > ?",
			codeHash, now.Unix())
		n, err := affected(res, err, "delete login code")
		if err != nil {
			return err
		}
		if n == 0 {
			return notFound("login code", "")
		}
		c.CreatedAt, c.ExpiresAt = fromUnix(created), fromUnix(exp)
		return nil
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.LoginCode{}, err
		}
		return model.LoginCode{}, fmt.Errorf("sqlstore: take login code: %w", err)
	}
	return c, nil
}

// DeleteUserLoginCodes deletes every code of the user.
func (d *DB) DeleteUserLoginCodes(ctx context.Context, userID int64) error {
	if _, err := d.q.ExecContext(ctx, "DELETE FROM web_login_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("sqlstore: delete login codes of user %d: %w", userID, err)
	}
	return nil
}

// ---- widget proofs ----

// UseProof deletes an expired row with the same hash, then inserts. A key
// conflict means that a live row exists: fresh=false.
func (d *DB) UseProof(ctx context.Context, hash string, expiresAt, now time.Time) (bool, error) {
	var fresh bool
	err := d.WithTx(ctx, func(tx store.Store) error {
		q := tx.(*DB).q
		if _, err := q.ExecContext(ctx, "DELETE FROM web_used_proofs WHERE hash = ? AND expires_at <= ?",
			hash, now.Unix()); err != nil {
			return fmt.Errorf("delete expired: %w", err)
		}
		res, err := q.ExecContext(ctx,
			d.d.InsertIgnore("web_used_proofs", []string{"hash", "expires_at"}, []string{"hash"}),
			hash, expiresAt.Unix())
		n, err := affected(res, err, "insert proof")
		fresh = n > 0
		return err
	})
	if err != nil {
		return false, fmt.Errorf("sqlstore: use proof: %w", err)
	}
	return fresh, nil
}

// ---- membership ----

const chatMemberCols = "chat_id, user_id, status, seen_at, checked_at"

func scanChatMember(s scanner) (model.ChatMember, error) {
	var m model.ChatMember
	var status string
	var seen, checked int64
	if err := s.Scan(&m.ChatID, &m.UserID, &status, &seen, &checked); err != nil {
		return model.ChatMember{}, err
	}
	m.Status, m.SeenAt, m.CheckedAt = model.MemberStatus(status), fromUnixOrZero(seen), fromUnixOrZero(checked)
	return m, nil
}

// NoteChatMember records a hint: a new row is 'unknown'; an old row gets
// only the new seen_at.
func (d *DB) NoteChatMember(ctx context.Context, chatID, userID int64, seenAt time.Time) error {
	_, err := d.q.ExecContext(ctx,
		d.d.Upsert("chat_members", []string{"chat_id", "user_id", "status", "seen_at", "checked_at"},
			[]string{"chat_id", "user_id"}, []string{"seen_at"}),
		chatID, userID, string(model.MemberUnknown), unixOrZero(seenAt), 0)
	if err != nil {
		return fmt.Errorf("sqlstore: note member %d of chat %d: %w", userID, chatID, err)
	}
	return nil
}

// SetChatMember writes status and checked_at, and keeps seen_at.
func (d *DB) SetChatMember(ctx context.Context, m model.ChatMember) error {
	_, err := d.q.ExecContext(ctx,
		d.d.Upsert("chat_members", []string{"chat_id", "user_id", "status", "seen_at", "checked_at"},
			[]string{"chat_id", "user_id"}, []string{"status", "checked_at"}),
		m.ChatID, m.UserID, string(m.Status), unixOrZero(m.SeenAt), unixOrZero(m.CheckedAt))
	if err != nil {
		return fmt.Errorf("sqlstore: set member %d of chat %d: %w", m.UserID, m.ChatID, err)
	}
	return nil
}

// GetChatMember returns the row of (chatID, userID).
func (d *DB) GetChatMember(ctx context.Context, chatID, userID int64) (model.ChatMember, error) {
	m, err := scanChatMember(d.q.QueryRowContext(ctx,
		"SELECT "+chatMemberCols+" FROM chat_members WHERE chat_id = ? AND user_id = ?", chatID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ChatMember{}, notFound("chat member", fmt.Sprintf("%d/%d", chatID, userID))
	}
	if err != nil {
		return model.ChatMember{}, fmt.Errorf("sqlstore: get member %d of chat %d: %w", userID, chatID, err)
	}
	return m, nil
}

// ListChatMembersOf returns every row of the user, ordered by chat_id.
func (d *DB) ListChatMembersOf(ctx context.Context, userID int64) ([]model.ChatMember, error) {
	rows, err := d.q.QueryContext(ctx,
		"SELECT "+chatMemberCols+" FROM chat_members WHERE user_id = ? ORDER BY chat_id", userID)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: list memberships of user %d: %w", userID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.ChatMember
	for rows.Next() {
		m, err := scanChatMember(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlstore: scan membership: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlstore: iterate memberships: %w", err)
	}
	return out, nil
}

// SetChatGone marks every row of the chat as 'left', checked at at.
func (d *DB) SetChatGone(ctx context.Context, chatID int64, at time.Time) error {
	if _, err := d.q.ExecContext(ctx, "UPDATE chat_members SET status = ?, checked_at = ? WHERE chat_id = ?",
		string(model.MemberLeft), at.Unix(), chatID); err != nil {
		return fmt.Errorf("sqlstore: set chat %d gone: %w", chatID, err)
	}
	return nil
}

// ListGroupChats returns up to limit group chats, ordered by id.
func (d *DB) ListGroupChats(ctx context.Context, limit int) ([]model.Chat, error) {
	if limit <= 0 {
		return nil, nil
	}
	return d.queryChats(ctx, "SELECT "+chatCols+" FROM chats WHERE kind = ? ORDER BY id LIMIT ?",
		string(model.ChatGroup), limit)
}

// purgeWeb is the web part of Purge (§6.4).
func (d *DB) purgeWeb(ctx context.Context, now time.Time) error {
	n := now.Unix()
	for _, p := range []struct {
		what, query string
		args        []any
	}{
		{"web sessions", "DELETE FROM web_sessions WHERE expires_at <= ? OR max_at <= ?", []any{n, n}},
		{"login codes", "DELETE FROM web_login_codes WHERE expires_at <= ?", []any{n}},
		{"used proofs", "DELETE FROM web_used_proofs WHERE expires_at <= ?", []any{n}},
		{"chat members", "DELETE FROM chat_members WHERE status <> ? AND seen_at < ? AND checked_at < ?",
			[]any{string(model.MemberYes), now.Add(-memberRetention).Unix(), now.Add(-memberRetention).Unix()}},
	} {
		if _, err := d.q.ExecContext(ctx, p.query, p.args...); err != nil {
			return fmt.Errorf("sqlstore: purge %s: %w", p.what, err)
		}
	}
	return nil
}
