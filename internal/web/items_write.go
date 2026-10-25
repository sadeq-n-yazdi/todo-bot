package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// The write API of docs/WEB-DESIGN.md §5.3 (endpoints 17 to 20) and §5.4.
// Each handler uses the actor of the middleware, never {chat}. The service
// checks that the item belongs to the chat of the actor, or that the user
// is its assignee and acts from the private chat (ARCHITECTURE §11). Each
// service write wakes the scheduler of this process (§10).

// The presets of "remind later" (endpoint 20).
const (
	preset10m      = "10m"
	preset1h       = "1h"
	presetTomorrow = "tomorrow"
)

type addRequest struct {
	Text       string `json:"text"`
	AssigneeID int64  `json:"assignee_id"`
}

type doneResponse struct {
	Item      itemDTO `json:"item"`
	StillOpen bool    `json:"still_open"`
}

type remindRequest struct {
	Preset *string `json:"preset"`
	When   *string `json:"when"`
}

type remindResponse struct {
	FireAt   time.Time `json:"fire_at"`
	FireText string    `json:"fire_text"`
}

// itemErr maps the errors of the service for one item. An item of another
// chat and a missing item both give 404 not_found, so a user cannot learn
// whether an item of another chat exists (§5.3).
func itemErr(err error) error {
	if errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrForeignChat) {
		return errNotFound
	}
	return err
}

// itemID parses {id}. A value that is not an int64 gives 404 not_found.
func itemID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errNotFound
	}
	return id, nil
}

// handleAddItem is endpoint 17, POST /api/chats/{chat}/items. It follows
// the steps of /add (§5.4): the digits, the limit of 1000 runes, the parse
// in the user's time zone, and the check of the assignee. The web does not
// split an @name at the start of the text: the form has an assignee picker.
func (s *Server) handleAddItem(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	text, err := itemText(req.Text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.parseItem(r, text)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(res.Text) == "" {
		s.fail(w, r, errEmptyText) // the text was only a date
		return
	}
	a := actorFrom(r.Context())
	if req.AssigneeID != 0 {
		ok, err := s.canAssign(r, req.AssigneeID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !ok {
			s.fail(w, r, errUnknownAssign)
			return
		}
	}
	td, err := s.d.Svc.AddTodoFor(r.Context(), a, req.AssigneeID, res)
	switch {
	case errors.Is(err, service.ErrEmptyText):
		err = errEmptyText
	case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrUnknownAssignee):
		err = errUnknownAssign // the user went away after the check
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.viewOf(r).toItem(td, ""))
}

// canAssign tells if the user of r can give an item to id: id must be a
// user of the allowlist that NamedUsers returns (§5.4 step 4).
func (s *Server) canAssign(r *http.Request, id int64) (bool, error) {
	us, err := s.assignees(r.Context(), sessionFrom(r).UserID)
	if err != nil {
		return false, err
	}
	for _, u := range us {
		if u.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// handleDoneItem is endpoint 18, POST /api/chats/{chat}/items/{id}/done.
// Complete follows §5 of ARCHITECTURE: an item that repeats can stay open
// with a new date, and still_open is then true. An item that is not open
// gives 404. When the assignee completes the item from another chat, the
// server sends the "did" notice to the chat of the item, one time.
func (s *Server) handleDoneItem(w http.ResponseWriter, r *http.Request) {
	if err := decodeEmpty(r); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := itemID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a := actorFrom(r.Context())
	before, err := s.d.Svc.Todo(r.Context(), a, id)
	if err == nil && before.Status != model.TodoOpen {
		err = service.ErrNotFound // done already: a second Done sends no notice
	}
	if err != nil {
		s.fail(w, r, itemErr(err))
		return
	}
	done, err := s.d.Svc.Complete(r.Context(), a, id)
	if err != nil {
		s.fail(w, r, itemErr(err))
		return
	}
	if changedByDone(before, done) && shouldNotifyDid(a, done) {
		s.notifyDid(r, done)
	}
	writeJSON(w, http.StatusOK, doneResponse{Item: s.viewOf(r).toItem(done, ""), StillOpen: done.Status == model.TodoOpen})
}

// changedByDone tells if Complete changed the item that was open in
// before: it is done now, or it repeats and has a new date. A repeating
// item that is due more than 12 hours later does not change.
func changedByDone(before, after model.Todo) bool {
	if after.Status != model.TodoOpen {
		return true
	}
	switch {
	case before.DueAt == nil && after.DueAt == nil:
		return false
	case before.DueAt == nil || after.DueAt == nil:
		return true
	}
	return !before.DueAt.Equal(*after.DueAt)
}

// decodeEmpty accepts a request with no body, or a body of "{}".
func decodeEmpty(r *http.Request) error {
	if r.ContentLength == 0 {
		return nil
	}
	var v struct{}
	return decodeJSON(r, &v)
}

// handleDeleteItem is endpoint 19, DELETE /api/chats/{chat}/items/{id}.
func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := itemID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.d.Svc.Delete(r.Context(), actorFrom(r.Context()), id); err != nil {
		s.fail(w, r, itemErr(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemindItem is endpoint 20, POST /api/chats/{chat}/items/{id}/remind.
// It adds a custom reminder with Service.Remind (§5.4): Service.Snooze
// needs a reminder ID, and the web has only items. The body has a preset
// (10m, 1h or tomorrow) or a "when" text, not both.
func (s *Server) handleRemindItem(w http.ResponseWriter, r *http.Request) {
	var req remindRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := itemID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v := s.viewOf(r)
	at, err := s.remindAt(req, v)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rem, err := s.d.Svc.Remind(r.Context(), actorFrom(r.Context()), id, at)
	if errors.Is(err, service.ErrInPast) {
		err = errInPast
	}
	if err != nil {
		s.fail(w, r, itemErr(err))
		return
	}
	writeJSON(w, http.StatusCreated, remindResponse{
		FireAt:   rem.FireAt.UTC(),
		FireText: ui.FormatWhen(rem.FireAt, v.lc, v.loc, v.now),
	})
}

// remindAt is the time of a "remind later" request, in the view v of the
// user:
//   - 10m and 1h: now plus the duration;
//   - tomorrow: the next calendar day in the user's time zone, at
//     TODO_DEFAULT_TIME, built with time.Date so that it is correct across
//     a change of DST;
//   - when: the text, after i18n.NormalizeDigits, through Parser.ParseWhen,
//     as /remind does. A text with no time, or a repeat, gives bad_time.
func (s *Server) remindAt(req remindRequest, v view) (time.Time, error) {
	switch {
	case (req.Preset == nil) == (req.When == nil):
		return time.Time{}, errBadRequest
	case req.When != nil:
		now := v.now.In(v.loc)
		at, rule, err := s.d.Parser.ParseWhen(strings.TrimSpace(i18n.NormalizeDigits(*req.When)), now, v.lc.Cal)
		if errors.Is(err, nlparse.ErrNoDate) || (err == nil && rule != nil) {
			return time.Time{}, errBadTime
		}
		return at, err
	}
	switch *req.Preset {
	case preset10m:
		return v.now.Add(10 * time.Minute), nil
	case preset1h:
		return v.now.Add(time.Hour), nil
	case presetTomorrow:
		y, m, d := v.now.In(v.loc).Date()
		return time.Date(y, m, d+1, s.d.Config.DefaultHour, s.d.Config.DefaultMinute, 0, 0, v.loc), nil
	}
	return time.Time{}, errBadRequest
}
