package service

import (
	"context"
	"errors"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
)

// Assignment (#58). This file is owned by the "service: assignment"
// follow-up PR (ARCHITECTURE §11); until it lands these methods are stubs
// and nothing calls them.

// errAssignPending is returned by the stubs below.
var errAssignPending = errors.New("service: assignment not implemented yet (#58)")

// NoteUsername implements Service.
func (s *Svc) NoteUsername(_ context.Context, _ int64, _ string) error {
	return errAssignPending
}

// UserByUsername implements Service.
func (s *Svc) UserByUsername(_ context.Context, _ string) (model.User, error) {
	return model.User{}, errAssignPending
}

// AddTodoFor implements Service.
func (s *Svc) AddTodoFor(_ context.Context, _ Actor, _ int64, _ nlparse.Result) (model.Todo, error) {
	return model.Todo{}, errAssignPending
}

// ListAssigned implements Service.
func (s *Svc) ListAssigned(_ context.Context, _ int64) ([]model.Todo, error) {
	return nil, errAssignPending
}
