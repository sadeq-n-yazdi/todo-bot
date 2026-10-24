package web

import (
	"log/slog"

	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Observe wraps the bot handler. It calls next for each update and then
// records a membership hint for a group update from an allowlisted user
// (docs/WEB-DESIGN.md §4.3).
//
// This is a W3 stub that only passes each update through. W4 replaces it.
func Observe(next tg.Handler, _ service.Web, _ map[int64]bool, _ *slog.Logger) tg.Handler {
	return next
}
