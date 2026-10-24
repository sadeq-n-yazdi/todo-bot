package handler

import (
	"context"
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Editing an item with the wizard (#98, ARCHITECTURE §13). The edit wizard
// is the create wizard with DraftData.EditTodoID set: it opens on the
// confirm step with the item as prefill, and Save sends only the parts that
// differ from DraftData.EditBase.

// editorKey carries the @username of the user who pressed the button, for
// the DM to a new assignee. It is request-scoped, never shared.
type editorKey struct{}

func editorName(ctx context.Context) string {
	s, _ := ctx.Value(editorKey{}).(string)
	return s
}

// editable says whether the edit wizard may start for td: the item must
// belong to the chat of the request and be open.
func editable(r req, td model.Todo) bool {
	return td.ChatID == r.Actor.ChatID && td.Status == model.TodoOpen
}

// startEdit sends the edit card for td to chatID and stores the draft. It
// replaces any earlier wizard of (chat, user). replyTo is the message to
// quote in a group (0 = none). The card is sent as a placeholder first,
// like startWizard does, because the version for its buttons is known only
// after StartDraft.
func (h *Handler) startEdit(ctx context.Context, r req, chatID int64, td model.Todo, replyTo int) error {
	lc := r.Prefs.Locale
	card := tg.OutMessage{
		Text:     lc.T("wizard.starting"),
		Keyboard: [][]tg.Button{{{Text: "⏳", Data: ui.Encode(ui.Action{Kind: ui.ActWizNoop})}}},
	}
	if r.Actor.ChatKind == model.ChatGroup {
		card.ReplyTo = replyTo
	}
	cardID, err := h.Send.Send(ctx, chatID, card)
	if err != nil {
		return err
	}
	base := h.editPrefill(r, td)
	data := base
	data.EditTodoID, data.EditNum = td.ID, td.Num
	data.Cal = string(calOf(lc))
	b := base
	data.EditBase = &b
	d := model.Draft{ChatID: chatID, UserID: r.Actor.UserID, MsgID: cardID, Step: model.StepConfirm, Data: data}
	if err := h.Svc.StartDraft(ctx, &d); err != nil {
		return err
	}
	h.draw(ctx, r, d, cardOpts{})
	return nil
}

// editPrefill converts td to wizard data in the editor's time zone and
// calendar. A schedule that the wizard cannot show exactly (it would not
// build the same rule or due time) goes into the Found* fields instead.
func (h *Handler) editPrefill(r req, td model.Todo) model.DraftData {
	dd := model.DraftData{Text: td.Text, OrigText: td.Text, AssigneeID: td.AssigneeID}
	if td.DueAt == nil {
		return dd
	}
	loc := tzOf(r)
	due := td.DueAt.In(loc)
	if td.Recur == "" && due.Hour() == 23 && due.Minute() == 59 {
		// A date-only item is stored at 23:59 (HasTime is not stored). The
		// wizard would show 23:59 as a chosen time and a later schedule
		// change would keep it, so show the item as a found date without
		// a time. An explicit "today 23:59" item looks the same: accepted.
		found := td.DueAt.UTC()
		dd.FoundDue, dd.FoundTime = &found, false
		return dd
	}
	dd.Date = due.Format("2006-01-02")
	if hh, mm := h.wizardTime(""); due.Hour() != hh || due.Minute() != mm {
		dd.Time = due.Format("15:04")
	}
	dd.Repeat = repeatKind(td.Recur)
	// A creator TZ that differs from the editor's TZ but gives the same rule
	// string prefills normally: any schedule change sets the editor's TZ, and
	// an untouched or text-only Save keeps the item's TZ.
	if h.roundTrips(r, dd, td) {
		return dd
	}
	found := td.DueAt.UTC()
	dd.Date, dd.Time, dd.Repeat = "", "", ""
	dd.FoundDue, dd.FoundRule, dd.FoundTime = &found, td.Recur, true
	return dd
}

// roundTrips says whether the wizard fields in dd build exactly the due
// time and rule of td.
func (h *Handler) roundTrips(r req, dd model.DraftData, td model.Todo) bool {
	loc := tzOf(r)
	day, err := time.ParseInLocation("2006-01-02", dd.Date, loc)
	if err != nil {
		return false
	}
	hh, mm := h.wizardTime(dd.Time)
	due := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
	if !due.Equal(td.DueAt.Truncate(time.Minute)) {
		return false
	}
	if td.Recur == "" {
		return dd.Repeat == model.RepeatOnce
	}
	if dd.Repeat == model.RepeatOnce {
		return false // a rule that the wizard has no choice for
	}
	rule, err := repeatRule(dd.Repeat, day.Year(), day.Month(), day.Day(), hh, mm, calOf(r.Prefs.Locale))
	return err == nil && rule.String() == td.Recur
}

// editDiff says which parts of an edit draft differ from its prefill.
type editDiff struct {
	text, schedule, assignee bool
}

func (e editDiff) any() bool { return e.text || e.schedule || e.assignee }

// normAssignee makes "no assignee" one value: in a private chat the actor's
// own ID and 0 are the same, as the service stores it.
func normAssignee(r req, id int64) int64 {
	if r.Actor.ChatKind == model.ChatPrivate && id == r.Actor.UserID {
		return 0
	}
	return id
}

// schedule is the schedule part of wizard data, with the fields that mean
// nothing without a date cleared.
type schedule struct {
	date, tm, repeat, rule string
	due                    *time.Time
	found                  bool
}

func scheduleOf(dd model.DraftData) schedule {
	var s schedule
	if dd.Date != "" {
		s.date, s.tm, s.repeat = dd.Date, dd.Time, dd.Repeat
	}
	if dd.FoundDue != nil {
		t := *dd.FoundDue
		s.due, s.rule, s.found = &t, dd.FoundRule, dd.FoundTime
	}
	return s
}

func (s schedule) equal(o schedule) bool {
	sameDue := (s.due == nil && o.due == nil) || (s.due != nil && o.due != nil && s.due.Equal(*o.due))
	return s.date == o.date && s.tm == o.tm && s.repeat == o.repeat && s.rule == o.rule && s.found == o.found && sameDue
}

func diffEdit(r req, d, base model.DraftData) editDiff {
	return editDiff{
		text:     d.Text != base.Text,
		assignee: normAssignee(r, d.AssigneeID) != normAssignee(r, base.AssigneeID),
		schedule: !scheduleOf(d).equal(scheduleOf(base)),
	}
}

// saveEdit is Save of an edit wizard.
func (h *Handler) saveEdit(ctx context.Context, r req, d model.Draft) (string, error) {
	lc := r.Prefs.Locale
	if d.Data.EditBase == nil {
		return "", errors.New("handler: edit draft without a base")
	}
	diff := diffEdit(r, d.Data, *d.Data.EditBase)

	finish := func(text string) {
		h.logErr("edit edit card", h.Send.Edit(ctx, d.ChatID, d.MsgID, tg.OutMessage{Text: text}))
	}
	end := func(text string) (string, error) {
		if err := h.Svc.EndDraft(ctx, d); err != nil {
			if errors.Is(err, service.ErrStaleDraft) {
				return h.staleToast(ctx, r, d.ChatID, d.MsgID), nil
			}
			return "", err
		}
		finish(text)
		return "", nil
	}
	if !diff.any() {
		return end(lc.T("wizard.edit.nochange"))
	}

	// The new assignee may have left the allowlist since it was chosen.
	if diff.assignee {
		if id := d.Data.AssigneeID; id != 0 && id != r.Actor.UserID && !h.Allowed[id] {
			return h.redoAssignee(ctx, r, d)
		}
	}
	var patch service.TodoPatch
	if diff.text {
		t := d.Data.Text
		patch.Text = &t
	}
	if diff.schedule {
		res, problem, err := h.buildResult(d, lc, tzOf(r), h.Clock.Now())
		if err != nil {
			return "", err
		}
		if nd, key, bad := problemRedo(problem, d); bad {
			return h.redoSave(ctx, r, d, nd, key)
		}
		res.Text = ""
		patch.When = &res
	}
	if diff.assignee {
		id := normAssignee(r, d.Data.AssigneeID)
		patch.AssigneeID = &id
	}

	td, err := h.Svc.SaveEditDraft(ctx, d, r.Actor, d.Data.EditTodoID, patch)
	switch {
	case errors.Is(err, service.ErrStaleDraft):
		return h.staleToast(ctx, r, d.ChatID, d.MsgID), nil
	case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrForeignChat):
		// The rolled-back transaction kept the draft: end it, or every
		// later Save would fail the same way.
		return end(lc.T("wizard.edit.gone"))
	case errors.Is(err, service.ErrUnknownAssignee):
		return h.redoAssignee(ctx, r, d)
	case err != nil:
		return "", err // the draft is still there: the user can press Save again
	}
	text := lc.T("todo.edited", lc.Num(td.Num), h.itemSummary(td, r))
	if diff.assignee {
		if note := h.notifyAssigned(ctx, r, td); note != "" {
			text += "\n" + note
		}
	}
	finish(text)
	return "", nil
}

// redoAssignee sends the draft back to the assignee step, with the
// assignee as it was before the edit.
func (h *Handler) redoAssignee(ctx context.Context, r req, d model.Draft) (string, error) {
	nd := d
	nd.Data.AssigneeID, nd.Step = d.Data.EditBase.AssigneeID, model.StepAssignee
	return h.redoSave(ctx, r, d, nd, "wizard.note.assignee")
}

// notifyAssigned sends one DM to the new assignee of an item after a Save
// that changed the assignee. It sends nothing when the new assignee is
// nobody or the editor. The text is in the assignee's language and
// calendar. There are no buttons and no retry. It returns a note for the
// editor's card when the assignee cannot be reached (never started the bot,
// or blocked it), otherwise "".
func (h *Handler) notifyAssigned(ctx context.Context, r req, td model.Todo) string {
	id := td.AssigneeID
	if id == 0 || id == r.Actor.UserID {
		return ""
	}
	ar := req{Prefs: model.Prefs{Locale: i18n.Default("")}}
	if p, err := h.Svc.Prefs(ctx, id, ""); err != nil {
		h.Log.Warn("assigned dm prefs", "user", id, "err", err)
	} else {
		ar.Prefs = p
	}
	alc := ar.Prefs.Locale
	shown := td
	shown.AssigneeID = 0 // the DM goes to the assignee: no "→ @them"
	text := alc.T("wizard.edit.assigned.dm",
		ui.Mention(r.Actor.UserID, editorName(ctx), alc), h.itemSummary(shown, ar))
	_, err := h.Send.Send(ctx, id, tg.OutMessage{Text: text})
	switch {
	case err == nil:
		return ""
	case tg.IsPermanent(err):
		h.Log.Info("assigned dm not delivered", "user", id, "err", err)
		return r.Prefs.Locale.T("wizard.edit.assigned.nodm", ui.Mention(id, td.AssigneeName, r.Prefs.Locale))
	default:
		h.Log.Warn("assigned dm", "user", id, "err", err)
		return ""
	}
}
