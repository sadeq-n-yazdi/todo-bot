package web

import "net/http"

// This file is a W3 stub. W8 (the write API) replaces it.

// handleSettings is endpoint 21, PUT /api/me/settings.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
