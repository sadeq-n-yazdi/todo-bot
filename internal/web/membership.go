package web

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Limits of docs/WEB-DESIGN.md §4.4 and §4.5.
const (
	// leftTTL is how long a "left" answer stays valid with no new call.
	leftTTL = time.Hour
	// memberGrace is how old a "member" row may be when Telegram has a
	// temporary error and the web still allows the call.
	memberGrace = time.Hour
	// memberCallsPerMin is the limit of getChatMember calls on one node.
	memberCallsPerMin = 30
	// memberCallTimeout is the time limit of one getChatMember call.
	memberCallTimeout = 3 * time.Second
	// defaultMemberTTL is used when the configuration has no TTL.
	defaultMemberTTL = 10 * time.Minute
)

// errTemp is the temporary error of §4.4 row g. It never reaches the
// client: the grace rule turns it into an allow, or checkMember turns it
// into errUnverified.
var errTemp = errors.New("web: membership check failed for now")

// flight is one getChatMember call that other requests can wait for.
type flight struct {
	done chan struct{}
	err  error // nil = member; an *apiError = final answer; or errTemp
}

type pairKey struct{ chat, user int64 }

// memberState is the in-memory state of the membership checks on this
// node. It only limits the calls to Telegram. The truth is in the
// chat_members rows, so any node can answer any request (§10).
type memberState struct {
	mu      sync.Mutex
	flights map[pairKey]*flight
	calls   *limiter // memberCallsPerMin, one key

	// callTimeout limits one Telegram call; tests make it short.
	callTimeout time.Duration
	// listTimeout stops all checks of GET /api/chats; zero means the default.
	listTimeout time.Duration
}

// initMembers sets up s.members. New calls it.
func (s *Server) initMembers() {
	s.members = memberState{
		flights:     map[pairKey]*flight{},
		calls:       newLimiter(s.d.Clock, memberCallsPerMin, time.Minute),
		callTimeout: memberCallTimeout,
	}
}

func (s *Server) memberTTL() time.Duration {
	if s.d.Config.WebMemberTTL > 0 {
		return s.d.Config.WebMemberTTL
	}
	return defaultMemberTTL
}

// groupActor gives the actor of a group, or one of the actor errors of
// §5.3 (docs/WEB-DESIGN.md §4.4). The title is that of the chats row.
func (s *Server) groupActor(ctx context.Context, userID, chatID int64) (service.Actor, error) {
	c, err := s.d.Web.Chat(ctx, chatID)
	switch {
	case errors.Is(err, service.ErrNotFound):
		return service.Actor{}, errNotFound
	case err != nil:
		return service.Actor{}, err
	case c.Kind != model.ChatGroup:
		return service.Actor{}, errNotFound
	}
	if _, err := s.membership(ctx, ctx, userID, chatID); err != nil {
		if errors.Is(err, errTemp) {
			return service.Actor{}, errUnverified
		}
		return service.Actor{}, err
	}
	return service.Actor{UserID: userID, ChatID: chatID, ChatKind: model.ChatGroup, ChatTitle: c.Title}, nil
}

// membership follows §4.4 steps 2 to 5. A nil error means allow. Other
// results are errNotMember, errNotFound, errGroupMoved (with new_id), an
// internal error, or errTemp when Telegram has a temporary error and the
// grace rule does not cover it. ctx is for the database; wait limits how
// long the call waits for Telegram. partial is true whenever Telegram had a
// temporary error, also when the grace rule allowed the call; then
// GET /api/chats says "partial".
func (s *Server) membership(ctx, wait context.Context, userID, chatID int64) (partial bool, err error) {
	row, err := s.d.Web.Membership(ctx, chatID, userID)
	haveRow := err == nil
	if err != nil && !errors.Is(err, service.ErrNotFound) {
		return false, err
	}
	now := s.d.Clock.Now()
	// A row is stale if the user wrote in the group after the last check.
	if haveRow && !row.CheckedAt.IsZero() && !row.SeenAt.After(row.CheckedAt) {
		age := now.Sub(row.CheckedAt)
		switch {
		case row.Status == model.MemberYes && age < s.memberTTL():
			return false, nil
		case row.Status == model.MemberLeft && age < leftTTL:
			return false, errNotMember
		}
	}
	err = s.askTelegram(ctx, wait, userID, chatID)
	if !errors.Is(err, errTemp) {
		return false, err
	}
	if haveRow && row.Status == model.MemberYes && !row.CheckedAt.IsZero() && now.Sub(row.CheckedAt) < memberGrace {
		return true, nil
	}
	return true, errTemp
}

// askTelegram shares one getChatMember call among all callers for the same
// pair. The call runs on its own context, so a caller that goes away does
// not stop it for the others. A caller stops waiting when wait is done.
func (s *Server) askTelegram(ctx, wait context.Context, userID, chatID int64) error {
	k := pairKey{chatID, userID}
	s.members.mu.Lock()
	f, ok := s.members.flights[k]
	if !ok {
		f = &flight{done: make(chan struct{})}
		s.members.flights[k] = f
		go func() {
			f.err = s.callTelegram(context.WithoutCancel(ctx), userID, chatID)
			s.members.mu.Lock()
			delete(s.members.flights, k)
			s.members.mu.Unlock()
			close(f.done)
		}()
	}
	s.members.mu.Unlock()
	select {
	case <-f.done:
		return f.err
	case <-wait.Done():
		return errTemp
	}
}

// callTelegram makes the call and writes the result (§4.4 steps 3 and 4).
// It never holds a database transaction while it waits for Telegram.
func (s *Server) callTelegram(ctx context.Context, userID, chatID int64) error {
	if ok, _ := s.members.calls.allow("getChatMember"); !ok {
		return errTemp
	}
	cctx, cancel := context.WithTimeout(ctx, s.members.callTimeout)
	defer cancel()
	m, err := s.d.Members.ChatMember(cctx, chatID, userID)
	if err != nil {
		return s.classifyError(ctx, userID, chatID, err)
	}
	switch m.Status {
	case tg.MemberCreator, tg.MemberAdministrator, tg.MemberMember: // row d
		return s.record(ctx, userID, chatID, model.MemberYes, nil)
	case tg.MemberRestricted: // rows e and f
		if m.IsMember {
			return s.record(ctx, userID, chatID, model.MemberYes, nil)
		}
		return s.record(ctx, userID, chatID, model.MemberLeft, errNotMember)
	case tg.MemberLeft, tg.MemberKicked: // row f
		return s.record(ctx, userID, chatID, model.MemberLeft, errNotMember)
	}
	s.log.Warn("web: unknown getChatMember status", "chat", chatID, "status", string(m.Status))
	return errTemp
}

// record stores a status and gives result, or an internal error.
func (s *Server) record(ctx context.Context, userID, chatID int64, st model.MemberStatus, result error) error {
	m := model.ChatMember{ChatID: chatID, UserID: userID, Status: st, CheckedAt: s.d.Clock.Now()}
	if err := s.d.Web.RecordMembership(ctx, m); err != nil {
		s.log.Error("web: record membership", "chat", chatID, "err", err)
		return errInternal
	}
	return result
}

// userErrors are the descriptions of §4.4 row b, in lower case: Telegram
// says that the user is the problem, not the chat.
var userErrors = []string{
	"user not found", "member not found", "participant_id_invalid",
	"user_id_invalid", "user is deactivated",
}

// classifyError applies rows a, b, c and g of the table of §4.4, in that
// order. The web does not use tg.IsPermanent, because it mixes user errors
// and chat errors.
func (s *Server) classifyError(ctx context.Context, userID, chatID int64, err error) error {
	var ae *tg.APIError
	if !errors.As(err, &ae) { // a timeout or a network error: row g
		return errTemp
	}
	desc := strings.ToLower(ae.Desc)
	switch {
	case ae.MigrateTo != 0: // row a
		if err := s.d.Svc.MigrateChat(ctx, chatID, ae.MigrateTo); err != nil {
			s.log.Warn("web: migrate chat", "old", chatID, "new", ae.MigrateTo, "err", err)
			return errTemp
		}
		if err := s.d.Web.ChatGone(ctx, chatID); err != nil {
			s.log.Warn("web: mark the old chat gone", "chat", chatID, "err", err)
		}
		return errGroupMoved.with("new_id", ae.MigrateTo)
	case (ae.Code == 400 || ae.Code == 403) && containsAny(desc, userErrors): // row b
		return s.record(ctx, userID, chatID, model.MemberLeft, errNotMember)
	case ae.Code == 403 || (ae.Code == 400 && strings.Contains(desc, "chat not found")): // row c
		if err := s.d.Web.ChatGone(ctx, chatID); err != nil {
			s.log.Error("web: mark chat gone", "chat", chatID, "err", err)
			return errInternal
		}
		return errNotFound
	}
	if ae.Code != 429 && ae.Code < 500 { // row g: log only an error that the table does not know
		s.log.Warn("web: getChatMember error", "chat", chatID, "code", ae.Code, "desc", ae.Desc)
	}
	return errTemp
}

func containsAny(s string, subs []string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}
