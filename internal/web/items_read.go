package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// maxAssigned is the most items that Assigned to me sends (§5.3, endpoint 11).
const maxAssigned = 200

// viewOf is the display context of the user of the request.
func (s *Server) viewOf(r *http.Request) view {
	p := sessionFrom(r).prefs
	loc := p.TZ
	if loc == nil {
		loc = time.UTC
	}
	return view{lc: p.Locale, loc: loc, now: s.d.Clock.Now()}
}

// handleListItems is endpoint 10, GET /api/chats/{chat}/items?page=N. It
// lists the open items of the chat of the actor. The actor middleware has
// checked that the user may see the chat; the handler never reads {chat}.
func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	page := 0
	if q := r.URL.Query().Get("page"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			s.fail(w, r, errBadRequest)
			return
		}
		page = n
	}
	a := actorFrom(r.Context())
	v := s.viewOf(r)
	todos, pages, err := s.d.Svc.List(r.Context(), a.ChatID, page)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page = min(page, pages-1) // the service clamps; say which page it gave
	l := listDTO{Items: v.toItems(todos, nil), Page: page, Pages: pages, RenderedAt: v.now.UTC()}
	if err := writeList(w, r, l); err != nil {
		s.fail(w, r, err)
	}
}

// handleAssigned is endpoint 11, GET /api/assigned. It lists the open items
// that other chats assigned to the user, at most maxAssigned, in one page.
// Each item has the title of its chat.
func (s *Server) handleAssigned(w http.ResponseWriter, r *http.Request) {
	uid := sessionFrom(r).UserID
	v := s.viewOf(r)
	todos, err := s.d.Svc.ListAssigned(r.Context(), uid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(todos) > maxAssigned {
		todos = todos[:maxAssigned]
	}
	titles := make(map[int64]string)
	for _, t := range todos {
		if _, ok := titles[t.ChatID]; ok {
			continue
		}
		c, err := s.d.Web.Chat(r.Context(), t.ChatID)
		switch {
		case err == nil:
			titles[t.ChatID] = c.Title
		case errors.Is(err, service.ErrNotFound):
			titles[t.ChatID] = ""
		default:
			s.fail(w, r, err)
			return
		}
	}
	l := listDTO{Items: v.toItems(todos, titles), Page: 0, Pages: 1, RenderedAt: v.now.UTC()}
	if err := writeList(w, r, l); err != nil {
		s.fail(w, r, err)
	}
}
