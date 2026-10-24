package web

import (
	"context"

	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// This file is a W3 stub. W4 (group membership, docs/WEB-DESIGN.md §4.4)
// replaces it.

// memberState is the in-memory state of the membership checks on this node:
// W4 puts the single call for each pair of user and group, and the node
// limit on getChatMember calls, here.
type memberState struct{}

// initMembers sets up s.members. New calls it.
func (s *Server) initMembers() { s.members = memberState{} }

// groupActor gives the actor of a group, or one of the actor errors of
// §5.3. The stub knows no member of any group, so it answers 404.
func (s *Server) groupActor(_ context.Context, _, _ int64) (service.Actor, error) {
	return service.Actor{}, errNotFound
}
