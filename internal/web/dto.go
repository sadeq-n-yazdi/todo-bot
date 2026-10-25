package web

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// assigneeDTO is the assignee of an item. Username is "" when the bot does
// not know it; the app then shows the web.item.noname string.
type assigneeDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// itemDTO is the item object of docs/WEB-DESIGN.md §5.2. The three due
// fields are null when the item has no date. ChatTitle is filled only by
// the Assigned to me list, and is "" in the other lists.
type itemDTO struct {
	ID        int64        `json:"id"`
	ChatID    int64        `json:"chat_id"`
	ChatTitle string       `json:"chat_title"`
	Num       int          `json:"num"`
	Text      string       `json:"text"`
	DueAt     *time.Time   `json:"due_at"`
	DueText   *string      `json:"due_text"`
	DueFull   *string      `json:"due_full"`
	Overdue   bool         `json:"overdue"`
	Recur     string       `json:"recur"`
	RecurText string       `json:"recur_text"`
	Assignee  *assigneeDTO `json:"assignee"`
	CreatedBy int64        `json:"created_by"`
}

// listDTO is the body of a list response (endpoints 10 and 11).
type listDTO struct {
	Items      []itemDTO `json:"items"`
	Page       int       `json:"page"`
	Pages      int       `json:"pages"`
	RenderedAt time.Time `json:"rendered_at"`
}

// view holds what the item object needs from the user and the clock.
type view struct {
	lc  i18n.Locale
	loc *time.Location
	now time.Time
}

// toItem builds the item object of t. chatTitle is "" except in the
// Assigned to me list.
func (v view) toItem(t model.Todo, chatTitle string) itemDTO {
	d := itemDTO{
		ID: t.ID, ChatID: t.ChatID, ChatTitle: chatTitle, Num: t.Num, Text: t.Text,
		Recur: t.Recur, CreatedBy: t.CreatedBy,
	}
	if t.Recur != "" {
		d.RecurText = ui.DescribeRule(t.Recur, v.lc)
	}
	if t.DueAt != nil {
		at := t.DueAt.UTC()
		rel := ui.FormatWhen(at, v.lc, v.loc, v.now)
		full := formatFull(at, v.lc, v.loc)
		d.DueAt, d.DueText, d.DueFull = &at, &rel, &full
		d.Overdue = t.Status == model.TodoOpen && t.DueAt.Before(v.now)
	}
	if t.AssigneeID != 0 {
		d.Assignee = &assigneeDTO{ID: t.AssigneeID, Username: t.AssigneeName}
	}
	return d
}

// toItems builds the items of a list. The result is never nil, so the JSON
// is [] and not null.
func (v view) toItems(ts []model.Todo, titles map[int64]string) []itemDTO {
	out := make([]itemDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, v.toItem(t, titles[t.ChatID]))
	}
	return out
}
