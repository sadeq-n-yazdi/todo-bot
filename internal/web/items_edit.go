package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// The edit API of docs/WEB-DESIGN.md §5.3 (endpoints 22 and 23) and §5.4.
// Both use the actor of the middleware, never {chat}. Only an open item can
// be read for the form or changed: a done or deleted item gives 404.

// editItemDTO is the item object of §5.2 with the two fields of the edit
// form. Status is always "open", because the routes give 404 for any other
// status. WhenText is a phrase that the parser reads back to the same
// schedule, for the "when" field of the form; it is "" when the item has no
// date, or when no such phrase exists (for example "every 3 days"). Note is
// only in the answer of endpoint 23: a plain text for the editor when the
// new assignee cannot get the message of the bot.
type editItemDTO struct {
	itemDTO
	Status   model.TodoStatus `json:"status"`
	WhenText string           `json:"when_text"`
	Note     string           `json:"note,omitempty"`
}

// patchRequest is the body of endpoint 23. A field that is not in the body
// does not change. When is raw, so that the handler can tell a missing
// "when" (no change) from "when": null (remove the date).
type patchRequest struct {
	Text       *string         `json:"text"`
	When       json.RawMessage `json:"when"`
	AssigneeID *int64          `json:"assignee_id"`
}

// openTodo reads the item id for the actor of r. An item of another chat, a
// missing item and an item that is not open all give 404 not_found.
func (s *Server) openTodo(r *http.Request) (model.Todo, error) {
	id, err := itemID(r)
	if err != nil {
		return model.Todo{}, err
	}
	t, err := s.d.Svc.Todo(r.Context(), actorFrom(r.Context()), id)
	if err != nil {
		return model.Todo{}, itemErr(err)
	}
	if t.Status != model.TodoOpen {
		return model.Todo{}, errNotFound
	}
	return t, nil
}

// handleGetItem is endpoint 22, GET /api/chats/{chat}/items/{id}. It gives
// the item for the edit form.
func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	t, err := s.openTodo(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.editItem(r, t))
}

// handlePatchItem is endpoint 23, PATCH /api/chats/{chat}/items/{id}. It
// reads the item first and keeps the old assignee. Then it builds a
// service.TodoPatch from the fields of the body (§5.4), and calls
// UpdateTodo. After a save that gave the item to a new assignee, it sends
// the message of #98 §4. An error of that send does not undo the save.
func (s *Server) handlePatchItem(w http.ResponseWriter, r *http.Request) {
	var req patchRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.openTodo(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.todoPatch(r, req, before)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	after, err := s.d.Svc.UpdateTodo(r.Context(), actorFrom(r.Context()), before.ID, p)
	switch {
	case errors.Is(err, service.ErrEmptyText):
		err = errEmptyText
	case errors.Is(err, service.ErrUnknownAssignee):
		err = errUnknownAssign // the user went away after the check
	}
	if err != nil {
		s.fail(w, r, itemErr(err))
		return
	}
	// Known issue: two PATCH requests at the same moment (for example on
	// two nodes) that both change the assignee can both send the message.
	// The bot avoids this with the version of the draft; the web has none.
	out := s.editItem(r, after)
	if shouldNotifyAssigned(before.AssigneeID, after, sessionFrom(r).UserID) {
		out.Note = s.notifyAssigned(r, after)
	}
	writeJSON(w, http.StatusOK, out)
}

// todoPatch builds the patch of req. A field that is not in req stays nil.
//   - text: the checks of Add (§5.4 steps 1 and 2); it is not parsed for a
//     date, because the form has its own "when" field.
//   - when: null removes the date. A text goes through Parser.Parse in the
//     time zone and the calendar of the user. The whole text must be one
//     date phrase (else bad_time), and a date with no repeat must be in the
//     future (else in_past). A repeat is moved forward by the service.
//   - assignee_id: 0 is nobody; another value is checked as in Add. The
//     current assignee needs no check, so the form can send it back.
func (s *Server) todoPatch(r *http.Request, req patchRequest, before model.Todo) (service.TodoPatch, error) {
	var p service.TodoPatch
	if req.Text != nil {
		text, err := itemText(*req.Text)
		if err != nil {
			return p, err
		}
		p.Text = &text
	}
	if req.When != nil {
		res, err := s.parseWhenField(r, req.When)
		if err != nil {
			return p, err
		}
		p.When = &res
	}
	if req.AssigneeID != nil {
		id := *req.AssigneeID
		if id != 0 && id != before.AssigneeID {
			ok, err := s.canAssign(r, id)
			if err != nil {
				return p, err
			}
			if !ok {
				return p, errUnknownAssign
			}
		}
		p.AssigneeID = &id
	}
	return p, nil
}

// parseWhenField reads the "when" field: null gives a result with no date,
// and a string is parsed as todoPatch says. Any other JSON value gives
// bad_request.
func (s *Server) parseWhenField(r *http.Request, raw json.RawMessage) (nlparse.Result, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nlparse.Result{}, nil
	}
	var when string
	if err := json.Unmarshal(raw, &when); err != nil {
		return nlparse.Result{}, errBadRequest
	}
	when = strings.TrimSpace(i18n.NormalizeDigits(when))
	if when == "" {
		return nlparse.Result{}, errBadTime // null removes the date
	}
	v := s.viewOf(r)
	res, err := s.d.Parser.Parse(when, v.now.In(v.loc), v.lc.Cal)
	switch {
	case err != nil, res.Due == nil && res.Rule == nil, strings.TrimSpace(res.Text) != "":
		return nlparse.Result{}, errBadTime
	case res.Rule == nil && !res.Due.After(v.now):
		return nlparse.Result{}, errInPast
	}
	res.Text = ""
	return res, nil
}

// editItem builds the answer of endpoints 22 and 23 for the user of r.
func (s *Server) editItem(r *http.Request, t model.Todo) editItemDTO {
	v := s.viewOf(r)
	return editItemDTO{itemDTO: v.toItem(t, ""), Status: t.Status, WhenText: s.whenText(t, v)}
}

// whenText is the "when" phrase of t for the form, in the time zone and
// the calendar of the view v (see editItemDTO). It uses English words and
// ASCII digits, which the parser reads in every language. Each phrase is
// parsed again before it is used: a phrase that does not give back the
// same schedule gives "".
func (s *Server) whenText(t model.Todo, v view) string {
	if t.DueAt == nil {
		return ""
	}
	now := v.now.In(v.loc)
	if t.Recur == "" {
		local := t.DueAt.In(v.loc)
		d := v.lc.Cal.Split(local)
		phrase := d.String()
		if local.Hour() != 23 || local.Minute() != 59 { // 23:59 is a date with no time
			phrase += local.Format(" 15:04")
		}
		res, err := s.d.Parser.Parse(phrase, now, v.lc.Cal)
		if err != nil || res.Rule != nil || res.Due == nil || !sameDay(*res.Due, *t.DueAt, v.loc) {
			return ""
		}
		return phrase
	}
	// The hour and the minute of a rule are in the zone of the item: the
	// phrase is read in the zone of the user, so the two must be the same.
	if t.TZ != v.loc.String() {
		return ""
	}
	rule, err := recur.Parse(t.Recur)
	if err != nil {
		return ""
	}
	phrase := rulePhrase(rule)
	if phrase == "" {
		return ""
	}
	res, err := s.d.Parser.Parse(phrase, now, v.lc.Cal)
	if err != nil || res.Rule == nil || res.Rule.String() != t.Recur || strings.TrimSpace(res.Text) != "" {
		return ""
	}
	return phrase
}

// sameDay tells if a and b are the same civil day and minute in loc. A
// date with no time parses to the default time, so for 23:59 only the day
// counts.
func sameDay(a, b time.Time, loc *time.Location) bool {
	a, b = a.In(loc), b.In(loc)
	if b.Hour() == 23 && b.Minute() == 59 {
		return a.Year() == b.Year() && a.YearDay() == b.YearDay()
	}
	return a.Equal(b)
}

// rulePhrase is an English phrase for r, or "" for a rule that has none.
// An every-N rule has none, because the parser starts it from now and the
// anchor of the item is lost.
func rulePhrase(r recur.Rule) string {
	at := fmt.Sprintf(" %02d:%02d", r.Hour, r.Minute)
	switch r.Freq {
	case recur.Daily:
		return "every day" + at
	case recur.Weekly:
		days := make([]string, 0, len(r.Weekdays))
		for _, d := range r.Weekdays {
			days = append(days, shortDay(d))
		}
		return "every " + strings.Join(days, ", ") + at
	case recur.Monthly:
		return "every month on the " + ordinal(r.MonthDay) + at
	case recur.MonthlyWeekday:
		if len(r.Weekdays) != 1 {
			return ""
		}
		nth := "last"
		if r.Nth != recur.Last {
			nth = ordinal(r.Nth)
		}
		return "every " + nth + " " + shortDay(r.Weekdays[0]) + " of the month" + at
	case recur.Yearly:
		cal := r.Cal
		if cal == "" {
			cal = calendar.Gregorian
		}
		return fmt.Sprintf("every year on %d %s", r.MonthDay, strings.ToLower(i18n.MonthNames(cal, i18n.En)[r.Month-1])) + at
	}
	return ""
}

func shortDay(d time.Weekday) string {
	return strings.ToLower(i18n.WeekdayNames(i18n.En)[d][:3])
}

// ordinal gives "1st", "2nd", "3rd", "4th", "11th", "22nd" and so on.
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
