package web

import (
	"context"
	"net/http"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

type memberDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type membersResponse struct {
	Members []memberDTO `json:"members"`
}

// assignees returns the people that userID can give an item to: the users
// with a known @username who are in the allowlist, without userID itself.
// It is the list of the assignee step of the wizard (docs/WEB-DESIGN.md
// §4.7), read from the database each time.
func (s *Server) assignees(ctx context.Context, userID int64) ([]model.User, error) {
	all, err := s.d.Svc.NamedUsers(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]model.User, 0, len(all))
	for _, u := range all {
		if s.d.Allowed[u.ID] && u.ID != userID {
			out = append(out, u)
		}
	}
	return out, nil
}

// handleMembers is endpoint 16, GET /api/chats/{chat}/members. It works for
// a group and for the private chat, because the Add form of Mine also has
// the assignee picker. The actor middleware has checked the chat.
func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	us, err := s.assignees(r.Context(), sessionFrom(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := membersResponse{Members: make([]memberDTO, 0, len(us))}
	for _, u := range us {
		out.Members = append(out.Members, memberDTO{ID: u.ID, Username: u.Username})
	}
	writeJSON(w, http.StatusOK, out)
}
