package web

import "net/http"

// This file is a W3 stub. W8 (the write API) replaces it.

// handleAddItem is endpoint 17, POST /api/chats/{chat}/items.
func (s *Server) handleAddItem(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handleDoneItem is endpoint 18, POST /api/chats/{chat}/items/{id}/done.
func (s *Server) handleDoneItem(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handleDeleteItem is endpoint 19, DELETE /api/chats/{chat}/items/{id}.
func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handleRemindItem is endpoint 20, POST /api/chats/{chat}/items/{id}/remind.
func (s *Server) handleRemindItem(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
