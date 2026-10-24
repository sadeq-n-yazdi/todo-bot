package service

import (
	"context"
	"errors"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Editing an item with the wizard (#98, ARCHITECTURE §13). These are stubs
// until the service part of #98 fills them in.

var errEditNotImplemented = errors.New("service: editing an item is not implemented")

// UpdateTodo implements Service.
func (s *Svc) UpdateTodo(ctx context.Context, a Actor, todoID int64, p TodoPatch) (model.Todo, error) {
	return model.Todo{}, errEditNotImplemented
}

// SaveEditDraft implements Service.
func (s *Svc) SaveEditDraft(ctx context.Context, d model.Draft, a Actor, todoID int64, p TodoPatch) (model.Todo, error) {
	return model.Todo{}, errEditNotImplemented
}
