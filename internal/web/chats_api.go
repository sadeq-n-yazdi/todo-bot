package web

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// Limits of docs/WEB-DESIGN.md §4.5.
const (
	maxGroupCandidates = 50 // Web.GroupChats limit
	listChecks         = 4  // checks at the same time
	defaultListTimeout = 5 * time.Second
)

type chatDTO struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type chatsResponse struct {
	Chats   []chatDTO `json:"chats"`
	Partial bool      `json:"partial"`
}

// handleChats is endpoint 9, GET /api/chats (§4.5). The private chat is
// first. The groups follow, sorted by title; only a confirmed member sees a
// group.
func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	uid := sessionFrom(r).UserID
	a, err := s.privateActor(r.Context(), uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cands, err := s.candidates(r.Context(), uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	groups, partial, err := s.checkAll(r.Context(), uid, cands)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Title != groups[j].Title {
			return groups[i].Title < groups[j].Title
		}
		return groups[i].ID < groups[j].ID
	})
	out := make([]chatDTO, 0, len(groups)+1)
	out = append(out, chatDTO{ID: a.ChatID, Kind: string(model.ChatPrivate), Title: a.ChatTitle})
	out = append(out, groups...)
	writeJSON(w, http.StatusOK, chatsResponse{Chats: out, Partial: partial})
}

// candidates gives the groups to check: every group in chats (at most 50)
// and every group of a chat_members row of the user that is still in
// chats. A chat ID that is not in chats is never a candidate.
func (s *Server) candidates(ctx context.Context, uid int64) ([]model.Chat, error) {
	groups, err := s.d.Web.GroupChats(ctx, maxGroupCandidates)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(groups))
	out := make([]model.Chat, 0, len(groups))
	for _, c := range groups {
		if c.Kind == model.ChatGroup && !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	rows, err := s.d.Web.MembershipsOf(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		if seen[m.ChatID] || m.ChatID >= 0 {
			continue
		}
		seen[m.ChatID] = true
		c, err := s.d.Web.Chat(ctx, m.ChatID)
		switch {
		case errors.Is(err, service.ErrNotFound):
			continue
		case err != nil:
			return nil, err
		case c.Kind == model.ChatGroup:
			out = append(out, c)
		}
	}
	return out, nil
}

// checkAll checks the candidates with at most listChecks at the same time
// and stops after the list timeout. A check that is not done counts as a
// temporary error.
func (s *Server) checkAll(ctx context.Context, uid int64, cands []model.Chat) (groups []chatDTO, partial bool, err error) {
	wait, cancel := context.WithTimeout(ctx, s.listTimeout())
	defer cancel()

	type result struct {
		partial bool
		err     error
	}
	res := make([]result, len(cands))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(listChecks, len(cands)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				p, err := s.membership(ctx, wait, uid, cands[i].ID)
				res[i] = result{p, err}
			}
		}()
	}
	for i := range cands {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for i, c := range cands {
		partial = partial || res[i].partial
		err := res[i].err
		var ae *apiError
		switch {
		case err == nil:
			groups = append(groups, chatDTO{ID: c.ID, Kind: string(model.ChatGroup), Title: c.Title})
		case errors.Is(err, errTemp), errors.As(err, &ae) && ae.Status != http.StatusInternalServerError:
			// not_member, not_found, group_moved or a temporary error:
			// the user does not see this group now.
		default:
			return nil, false, err
		}
	}
	return groups, partial, nil
}

func (s *Server) listTimeout() time.Duration {
	if s.members.listTimeout > 0 {
		return s.members.listTimeout
	}
	return defaultListTimeout
}
