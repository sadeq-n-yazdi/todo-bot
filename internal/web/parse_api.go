package web

import "net/http"

// This file is a W3 stub. W8 (the write API) replaces it.

// handleParse is endpoint 15, POST /api/parse.
func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
