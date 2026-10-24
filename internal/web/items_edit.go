package web

import "net/http"

// This file is a W3 stub. W11 (the edit API) replaces it.

// handleGetItem is endpoint 22, GET /api/chats/{chat}/items/{id}.
func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handlePatchItem is endpoint 23, PATCH /api/chats/{chat}/items/{id}.
func (s *Server) handlePatchItem(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
