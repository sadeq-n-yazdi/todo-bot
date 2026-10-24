package web

import "net/http"

// This file is a W3 stub. W8 (the write API) replaces it.

// handleMembers is endpoint 16, GET /api/chats/{chat}/members.
func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
