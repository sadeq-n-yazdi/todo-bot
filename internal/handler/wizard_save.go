package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Save: the draft becomes an item through the same code as /add, and the
// draft is deleted in the same transaction (Svc.SaveDraft).

// saveProblem says why a draft cannot be saved as it is.
type saveProblem int

const (
	saveOK        saveProblem = iota
	saveTimePast              // a picked date's time has passed: back to time
	saveDatePast              // a picked date is before today: back to date
	saveFoundPast             // a found date has passed: back to date
)

// wizardTime parses "HH:MM" or returns the default time.
func (h *Handler) wizardTime(s string) (hh, mm int) {
	if s == "" {
		return h.Parser.DefaultHour, h.Parser.DefaultMinute
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return h.Parser.DefaultHour, h.Parser.DefaultMinute
	}
	return t.Hour(), t.Minute()
}

func sameCivilDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// repeatRule builds the rule of a repeating choice for the date (y, m, d)
// at hh:mm from its canonical string, so only a rule recur can read back is
// ever stored. cal is the user's calendar: monthly and yearly rules use it.
func repeatRule(repeat string, y int, m time.Month, d, hh, mm int, cal calendar.Kind) (recur.Rule, error) {
	civil := time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	cd := cal.Split(civil)
	rule := recur.Rule{Cal: calendar.Gregorian, Hour: hh, Minute: mm}
	switch repeat {
	case model.RepeatDaily:
		rule.Freq = recur.Daily
	case model.RepeatWeekly:
		rule.Freq, rule.Weekdays = recur.Weekly, []time.Weekday{civil.Weekday()}
	case model.RepeatMonthly:
		rule.Freq, rule.MonthDay, rule.Cal = recur.Monthly, cd.Day, cal
	case model.RepeatYearly:
		rule.Freq, rule.Month, rule.MonthDay, rule.Cal = recur.Yearly, cd.Month, cd.Day, cal
	default:
		return recur.Rule{}, fmt.Errorf("handler: unknown repeat %q", repeat)
	}
	return recur.Parse(rule.String())
}

// buildResult maps the draft to the nlparse.Result that /add would have
// produced, or says why it cannot (saveProblem). Only a once item is
// refused for a time that has passed, or given the Today default; a
// repeating item keeps the date at hh:mm and the service advances it.
func (h *Handler) buildResult(d model.Draft, lc i18n.Locale, loc *time.Location, now time.Time) (nlparse.Result, saveProblem, error) {
	dd := d.Data
	res := nlparse.Result{Text: dd.Text}
	now = now.In(loc)
	if dd.Date == "" {
		if dd.FoundDue == nil {
			return res, saveOK, nil // no date
		}
		due := *dd.FoundDue
		res.Due, res.HasTime = &due, dd.FoundTime
		if dd.FoundRule != "" {
			rule, err := recur.Parse(dd.FoundRule)
			if err != nil {
				return res, saveOK, err
			}
			res.Rule = &rule
			return res, saveOK, nil
		}
		if !due.After(now) {
			if dd.FoundTime || !sameCivilDay(due.In(loc), now) {
				return res, saveFoundPast, nil
			}
			end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, loc)
			res.Due, res.HasTime = &end, false
		}
		return res, saveOK, nil
	}

	day, err := time.ParseInLocation("2006-01-02", dd.Date, loc)
	if err != nil {
		return res, saveOK, fmt.Errorf("handler: draft date %q: %w", dd.Date, err)
	}
	hh, mm := h.wizardTime(dd.Time)
	due := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
	res.Due, res.HasTime = &due, dd.Time != ""
	if dd.Repeat != model.RepeatOnce {
		rule, err := repeatRule(dd.Repeat, day.Year(), day.Month(), day.Day(), hh, mm, calOf(lc))
		if err != nil {
			return res, saveOK, err
		}
		res.Rule = &rule
		return res, saveOK, nil
	}
	if due.After(now) {
		return res, saveOK, nil
	}
	switch {
	case dd.Time == "" && sameCivilDay(due, now):
		// Today's default slot has passed: end of today, no time, like /add.
		end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, loc)
		res.Due, res.HasTime = &end, false
	case dateKey(day.Year(), day.Month(), day.Day()) < dateKey(now.Year(), now.Month(), now.Day()):
		return res, saveDatePast, nil
	default:
		return res, saveTimePast, nil
	}
	return res, saveOK, nil
}

// save validates the draft and creates the item. It returns the toast.
func (h *Handler) save(ctx context.Context, r req, d model.Draft) (string, error) {
	lc := r.Prefs.Locale
	redo := func(nd model.Draft, noteKey string) (string, error) {
		nd.Data.Editing = true
		stale, err := h.commit(ctx, r, d, nd, cardOpts{Note: lc.T(noteKey)})
		if err != nil {
			return "", err
		}
		if stale {
			return h.staleToast(ctx, r, d.ChatID, d.MsgID), nil
		}
		return "", nil
	}

	// The assignee may have left the allowlist since it was chosen.
	if id := d.Data.AssigneeID; id != 0 && id != r.Actor.UserID && !h.Allowed[id] {
		nd := d
		nd.Data.AssigneeID, nd.Step = 0, model.StepAssignee
		return redo(nd, "wizard.note.assignee")
	}
	res, problem, err := h.buildResult(d, lc, tzOf(r), h.Clock.Now())
	if err != nil {
		return "", err
	}
	switch problem {
	case saveTimePast:
		nd := d
		nd.Step = model.StepTime
		return redo(nd, "wizard.note.passed")
	case saveDatePast:
		nd := d
		nd.Step = model.StepDate
		return redo(nd, "wizard.note.passed")
	case saveFoundPast:
		nd := d
		clearFound(&nd)
		nd.Data.Text = d.Data.OrigText
		nd.Data.Date, nd.Data.Time, nd.Data.Repeat = "", "", ""
		nd.Step = model.StepDate
		return redo(nd, "wizard.note.passed")
	}

	td, err := h.Svc.SaveDraft(ctx, d, r.Actor, d.Data.AssigneeID, res)
	if errors.Is(err, service.ErrStaleDraft) {
		return h.staleToast(ctx, r, d.ChatID, d.MsgID), nil
	}
	if err != nil {
		return "", err // the draft is still there: the user can press Save again
	}
	text := lc.T("todo.added", lc.Num(td.Num), h.itemSummary(td, r))
	h.logErr("edit saved card", h.Send.Edit(ctx, d.ChatID, d.MsgID, tg.OutMessage{Text: text}))
	return "", nil
}
