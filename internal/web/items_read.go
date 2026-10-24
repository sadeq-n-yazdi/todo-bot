package web

import "net/http"

// This file is a W3 stub. W5 (the read API) replaces it.

// handleListItems is endpoint 10, GET /api/chats/{chat}/items.
func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handleAssigned is endpoint 11, GET /api/assigned.
func (s *Server) handleAssigned(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
