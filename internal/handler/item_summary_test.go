package handler

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func TestItemSummaryShowsRule(t *testing.T) {
	h := &Handler{Clock: clocktest.New(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))}
	r := req{Prefs: model.Prefs{Locale: i18n.Locale{Lang: i18n.En}}}
	if got := h.itemSummary(model.Todo{Text: "pay", Recur: "weekly mon,wed 08:00"}, r); got != "pay 🔁 every Mon, Wed" {
		t.Errorf("got %q", got)
	}
	if got := h.itemSummary(model.Todo{Text: "pay", Recur: "bogus"}, r); got != "pay 🔁" {
		t.Errorf("fallback: %q", got)
	}
}
