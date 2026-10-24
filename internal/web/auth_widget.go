package web

import "net/http"

// This file is a W3 stub. W13 (the Login Widget, docs/WEB-DESIGN.md §3.4)
// replaces it.

// handleWidget is endpoint 24, GET /auth/telegram.
func (s *Server) handleWidget(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
