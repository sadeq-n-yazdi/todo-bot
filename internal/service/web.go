package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// Web is the API of internal/web (docs/WEB-DESIGN.md). *Svc implements it.
type Web interface {
	// Sessions. The token is never stored; the store keeps its SHA-256.
	// StartWebSession trims the sessions of the same kind (5 miniapp, 20 browser).
	StartWebSession(ctx context.Context, userID int64, kind model.WebSessionKind,
		device string, idle, max time.Duration) (token string, s model.WebSession, err error)
	// WebSession returns the live session of token (ErrNotFound if none). For a
	// browser session it slides the expiry, with at most one write per hour.
	WebSession(ctx context.Context, token string, idle time.Duration) (model.WebSession, error)
	EndWebSession(ctx context.Context, token string) error
	EndUserWebSessions(ctx context.Context, userID int64) error
	PruneWebSessions(ctx context.Context, allowed map[int64]bool) (int, error)

	// Login codes (§3.3). internal/web verifies init_data before NewLoginCode.
	NewLoginCode(ctx context.Context, userID int64) (code string, c model.LoginCode, err error) // ErrTooManyCodes
	// RedeemLoginCode normalises code and takes the row. ErrNotFound if the
	// code is unknown, expired or used.
	RedeemLoginCode(ctx context.Context, code string) (model.LoginCode, error)
	CancelLoginCodes(ctx context.Context, userID int64) error

	// Login Widget proofs (§3.4). fresh=false if the proof was used before.
	UseProof(ctx context.Context, hash string, expiresAt time.Time) (fresh bool, err error)

	// Membership (§4). The service only stores; internal/web calls Telegram.
	NoteMember(ctx context.Context, a Actor) error
	Membership(ctx context.Context, chatID, userID int64) (model.ChatMember, error)
	MembershipsOf(ctx context.Context, userID int64) ([]model.ChatMember, error)
	RecordMembership(ctx context.Context, m model.ChatMember) error
	ChatGone(ctx context.Context, chatID int64) error
	GroupChats(ctx context.Context, limit int) ([]model.Chat, error)

	// Reads that the bot gets from Telegram updates.
	Chat(ctx context.Context, chatID int64) (model.Chat, error)
	User(ctx context.Context, userID int64) (model.User, error)
}

// ErrTooManyCodes: maxLiveCodes login codes of other users are live
// (NewLoginCode).
var ErrTooManyCodes = errors.New("too many live login codes")

var _ Web = (*Svc)(nil)

// Limits of the web (docs/WEB-DESIGN.md §3.3, §3.5, §4.3). The signatures
// of Web carry no such arguments, so they live here.
const (
	// tokenPrefix starts every session token; tokenLen is its length:
	// the prefix and 32 random bytes as base64url without padding.
	tokenPrefix = "tb1_"
	tokenLen    = len(tokenPrefix) + 43
	// maxMiniAppSessions and maxBrowserSessions are the session limits of
	// one user, for each kind on its own.
	maxMiniAppSessions = 5
	maxBrowserSessions = 20
	// slideEvery is how old seen_at must be before WebSession slides the
	// expiry of a browser session again.
	slideEvery = time.Hour
	// maxDevice is the length limit of WebSession.Device, in characters.
	maxDevice = 120
	// loginCodeTTL is the life of a login code; maxLiveCodes is the cap
	// on live codes of all users together.
	loginCodeTTL = 5 * time.Minute
	maxLiveCodes = 100
	// loginCodeLen is the length of a login code without the dash.
	loginCodeLen = 8
	// noteEvery is how old seen_at must be before NoteMember writes again.
	noteEvery = time.Hour
)

// crockford is the Crockford base32 alphabet of login codes.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// hashHex returns the hex SHA-256 of s: the only form in which tokens and
// codes reach the store.
func hashHex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("service: session token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// tokenID returns the session ID of token, or ok=false if token does not
// have the form of a session token.
func tokenID(token string) (string, bool) {
	if len(token) != tokenLen || !strings.HasPrefix(token, tokenPrefix) {
		return "", false
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(token[len(tokenPrefix):]); err != nil {
		return "", false
	}
	return hashHex(token), true
}

func sessionLimit(kind model.WebSessionKind) (int, error) {
	switch kind {
	case model.WebMiniApp:
		return maxMiniAppSessions, nil
	case model.WebBrowser:
		return maxBrowserSessions, nil
	}
	return 0, fmt.Errorf("service: unknown web session kind %q", kind)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// StartWebSession implements Web. The expiry is now+idle, but never after
// now+max. In one transaction it trims the sessions of the same kind to one
// below the limit and then inserts the new one, so the new session always
// stays, also when several start in the same second.
func (s *Svc) StartWebSession(ctx context.Context, userID int64, kind model.WebSessionKind,
	device string, idle, max time.Duration) (string, model.WebSession, error) {
	limit, err := sessionLimit(kind)
	if err != nil {
		return "", model.WebSession{}, err
	}
	if idle <= 0 || max <= 0 {
		return "", model.WebSession{}, fmt.Errorf("service: web session limits must be positive (idle %v, max %v)", idle, max)
	}
	token, err := newToken()
	if err != nil {
		return "", model.WebSession{}, err
	}
	now := s.clk.Now().UTC().Truncate(time.Second)
	maxAt := now.Add(max)
	ws := model.WebSession{
		ID: hashHex(token), UserID: userID, Kind: kind, Device: truncateRunes(device, maxDevice),
		CreatedAt: now, SeenAt: now, ExpiresAt: earlier(now.Add(idle), maxAt), MaxAt: maxAt,
	}
	err = s.st.WithTx(ctx, func(tx store.Store) error {
		if err := tx.TrimUserWebSessions(ctx, userID, kind, limit-1); err != nil {
			return err
		}
		return tx.PutWebSession(ctx, ws)
	})
	if err != nil {
		return "", model.WebSession{}, err
	}
	return token, ws, nil
}

// WebSession implements Web. It writes only when seen_at is slideEvery old
// or more, so a busy user causes at most one write per hour.
func (s *Svc) WebSession(ctx context.Context, token string, idle time.Duration) (model.WebSession, error) {
	id, ok := tokenID(token)
	if !ok {
		return model.WebSession{}, fmt.Errorf("service: web session: %w", ErrNotFound)
	}
	now := s.clk.Now().UTC().Truncate(time.Second)
	ws, err := s.st.GetWebSession(ctx, id, now)
	if err != nil {
		return model.WebSession{}, err
	}
	if ws.Kind != model.WebBrowser || now.Sub(ws.SeenAt) < slideEvery || idle <= 0 {
		return ws, nil
	}
	exp := earlier(now.Add(idle), ws.MaxAt)
	if err := s.st.TouchWebSession(ctx, id, now, exp); err != nil {
		return model.WebSession{}, err // ErrNotFound: a logout won the race
	}
	ws.SeenAt, ws.ExpiresAt = now, exp
	return ws, nil
}

// EndWebSession implements Web. A token that is not live is not an error.
func (s *Svc) EndWebSession(ctx context.Context, token string) error {
	id, ok := tokenID(token)
	if !ok {
		return nil
	}
	return s.st.DeleteWebSession(ctx, id)
}

// EndUserWebSessions implements Web ("log out everywhere").
func (s *Svc) EndUserWebSessions(ctx context.Context, userID int64) error {
	return s.st.DeleteUserWebSessions(ctx, userID)
}

// PruneWebSessions implements Web: it deletes the sessions of every user
// who is not allowed (allowed[id] false or missing).
func (s *Svc) PruneWebSessions(ctx context.Context, allowed map[int64]bool) (int, error) {
	keep := make([]int64, 0, len(allowed))
	for id, ok := range allowed {
		if ok {
			keep = append(keep, id)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i] < keep[j] })
	return s.st.DeleteWebSessionsExcept(ctx, keep)
}

// newLoginCode returns 8 random Crockford base32 characters (40 bits).
func newLoginCode() (string, error) {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("service: login code: %w", err)
	}
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	out := make([]byte, loginCodeLen)
	for i := loginCodeLen - 1; i >= 0; i-- {
		out[i] = crockford[v&31]
		v >>= 5
	}
	return string(out), nil
}

// normLoginCode is the normalisation of §3.3 step 6: it converts Persian
// and Arabic-Indic digits, removes spaces and "-", changes letters to upper
// case, I and L to 1, and O to 0.
func normLoginCode(code string) string {
	code = i18n.NormalizeDigits(code)
	var b strings.Builder
	for _, r := range code {
		if unicode.IsSpace(r) || r == '-' {
			continue
		}
		switch r = unicode.ToUpper(r); r {
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func validLoginCode(code string) bool {
	if len(code) != loginCodeLen {
		return false
	}
	for i := range len(code) {
		if !strings.ContainsRune(crockford, rune(code[i])) {
			return false
		}
	}
	return true
}

// NewLoginCode implements Web. It returns the code as 8 characters with no
// dash; internal/web shows it as ABCD-EFGH. The store gets only its hash.
// A new code deletes the other codes of the user. ErrTooManyCodes if
// maxLiveCodes codes of other users are live.
func (s *Svc) NewLoginCode(ctx context.Context, userID int64) (string, model.LoginCode, error) {
	code, err := newLoginCode()
	if err != nil {
		return "", model.LoginCode{}, err
	}
	now := s.clk.Now().UTC().Truncate(time.Second)
	c := model.LoginCode{CodeHash: hashHex(code), UserID: userID, CreatedAt: now, ExpiresAt: now.Add(loginCodeTTL)}
	ok, err := s.st.PutLoginCode(ctx, c, maxLiveCodes, now)
	if err != nil {
		return "", model.LoginCode{}, err
	}
	if !ok {
		return "", model.LoginCode{}, ErrTooManyCodes
	}
	return code, c, nil
}

// RedeemLoginCode implements Web. Of several concurrent redeems of one code
// (on any node) exactly one gets the row.
func (s *Svc) RedeemLoginCode(ctx context.Context, code string) (model.LoginCode, error) {
	code = normLoginCode(code)
	if !validLoginCode(code) {
		return model.LoginCode{}, fmt.Errorf("service: login code: %w", ErrNotFound)
	}
	return s.st.TakeLoginCode(ctx, hashHex(code), s.clk.Now().UTC())
}

// CancelLoginCodes implements Web.
func (s *Svc) CancelLoginCodes(ctx context.Context, userID int64) error {
	return s.st.DeleteUserLoginCodes(ctx, userID)
}

// UseProof implements Web. hash is already the SHA-256 of the widget hash.
func (s *Svc) UseProof(ctx context.Context, hash string, expiresAt time.Time) (bool, error) {
	return s.st.UseProof(ctx, hash, expiresAt.UTC(), s.clk.Now().UTC())
}

// NoteMember implements Web. For a group actor it records the hint of §4.2
// and the chat title. It reads the row first and writes only when there is
// no row or seen_at is noteEvery old or more. An empty title is not written,
// so it never erases the stored name. Other actors do nothing.
func (s *Svc) NoteMember(ctx context.Context, a Actor) error {
	if a.ChatKind != model.ChatGroup {
		return nil
	}
	now := s.clk.Now().UTC()
	m, err := s.st.GetChatMember(ctx, a.ChatID, a.UserID)
	switch {
	case err == nil:
		if now.Sub(m.SeenAt) < noteEvery {
			return nil
		}
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	if a.ChatTitle != "" {
		if err := s.st.EnsureChat(ctx, model.Chat{ID: a.ChatID, Kind: model.ChatGroup, Title: a.ChatTitle, CreatedAt: now}); err != nil {
			return err
		}
	}
	return s.st.NoteChatMember(ctx, a.ChatID, a.UserID, now)
}

// Membership implements Web; ErrNotFound if there is no row.
func (s *Svc) Membership(ctx context.Context, chatID, userID int64) (model.ChatMember, error) {
	return s.st.GetChatMember(ctx, chatID, userID)
}

// MembershipsOf implements Web: every row of the user, ordered by chat ID.
func (s *Svc) MembershipsOf(ctx context.Context, userID int64) ([]model.ChatMember, error) {
	return s.st.ListChatMembersOf(ctx, userID)
}

// RecordMembership implements Web: it stores the answer of getChatMember.
// A zero CheckedAt means now. SeenAt of an existing row does not change.
func (s *Svc) RecordMembership(ctx context.Context, m model.ChatMember) error {
	if m.CheckedAt.IsZero() {
		m.CheckedAt = s.clk.Now().UTC()
	}
	return s.st.SetChatMember(ctx, m)
}

// ChatGone implements Web: every row of the chat becomes 'left', checked now.
func (s *Svc) ChatGone(ctx context.Context, chatID int64) error {
	return s.st.SetChatGone(ctx, chatID, s.clk.Now().UTC())
}

// GroupChats implements Web: up to limit group chats, ordered by ID.
func (s *Svc) GroupChats(ctx context.Context, limit int) ([]model.Chat, error) {
	return s.st.ListGroupChats(ctx, limit)
}

// Chat implements Web; ErrNotFound if the bot does not know the chat.
func (s *Svc) Chat(ctx context.Context, chatID int64) (model.Chat, error) {
	return s.st.GetChat(ctx, chatID)
}

// User implements Web; ErrNotFound if the bot does not know the user.
func (s *Svc) User(ctx context.Context, userID int64) (model.User, error) {
	return s.st.GetUser(ctx, userID)
}
