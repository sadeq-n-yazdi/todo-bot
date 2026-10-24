package web

import (
	"net/http"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// This file is a W3 stub. W4 replaces it with the full list of §4.5.

type chatDTO struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type chatsResponse struct {
	Chats   []chatDTO `json:"chats"`
	Partial bool      `json:"partial"`
}

// handleChats is endpoint 9, GET /api/chats. The stub gives only the
// user's private chat.
func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	a, err := s.privateActor(r.Context(), sessionFrom(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chatsResponse{
		Chats: []chatDTO{{ID: a.ChatID, Kind: string(model.ChatPrivate), Title: a.ChatTitle}},
	})
}
