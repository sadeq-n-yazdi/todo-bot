package web

import "net/http"

// This file is a W3 stub. W7 (the login code, docs/WEB-DESIGN.md §3.3)
// replaces it.

// codeState is the in-memory state of the login codes on this node: W7
// puts the redeem failure limits (per IP and per node) here.
type codeState struct{}

// initCodes sets up s.codes. New calls it.
func (s *Server) initCodes() { s.codes = codeState{} }

// handleNewCode is endpoint 12, POST /api/auth/code (Mini App only).
func (s *Server) handleNewCode(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handleRedeemCode is endpoint 13, POST /api/auth/code/redeem.
func (s *Server) handleRedeemCode(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}

// handleCancelCode is endpoint 14, DELETE /api/auth/code (Mini App only).
func (s *Server) handleCancelCode(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, errNotImplemented)
}
