package handler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// The wizard's button presses and transitions (ARCHITECTURE §12). Every
// transition works on a copy of the stored draft and is stored with
// Svc.UpdateDraft; nothing is kept in memory.

// wizardWindowYears is how far ahead a date or month may be chosen.
const wizardWindowYears = 5

// cardOpts says how a card is drawn, and what a press did.
type cardOpts struct {
	Note     string         // plain text: ui escapes it
	Grid     *calendar.Date // month grid view of the date step
	EditMenu bool           // edit menu view of the confirm step
	view     bool           // nothing to store: only draw
	done     bool           // the press ended the wizard itself (cancel, save)
}

// calOf is the user's calendar, Gregorian when unset.
func calOf(lc i18n.Locale) calendar.Kind {
	if lc.Cal.Valid() {
		return lc.Cal
	}
	return calendar.Gregorian
}

func dateKey(y int, m time.Month, d int) int { return y*10000 + int(m)*100 + d }

func knownStep(s model.DraftStep) bool {
	switch s {
	case model.StepText, model.StepFound, model.StepDate, model.StepTime, model.StepTimeText,
		model.StepRepeat, model.StepAssignee, model.StepConfirm:
		return true
	}
	return false
}

func hasFound(d model.Draft) bool { return d.Data.FoundDue != nil }

// ---- drawing ----

// members returns the people who may be assigned items by actorID: users
// with a username, on the allowlist. It is read from the store each time.
func (h *Handler) members(ctx context.Context, actorID int64) ([]model.User, error) {
	all, err := h.Svc.NamedUsers(ctx, actorID)
	if err != nil {
		return nil, err
	}
	out := make([]model.User, 0, len(all))
	for _, u := range all {
		if h.Allowed[u.ID] && u.ID != actorID {
			out = append(out, u)
		}
	}
	return out, nil
}

func (h *Handler) cardMessage(ctx context.Context, r req, d model.Draft, o cardOpts) (tg.OutMessage, error) {
	v := ui.WizardView{
		Draft:       d,
		Grid:        o.Grid,
		EditMenu:    o.EditMenu,
		Note:        o.Note,
		DefaultTime: fmt.Sprintf("%02d:%02d", h.Parser.DefaultHour, h.Parser.DefaultMinute),
		Group:       r.Actor.ChatKind == model.ChatGroup,
	}
	if d.Step == model.StepAssignee || d.Step == model.StepConfirm {
		ms, err := h.members(ctx, r.Actor.UserID)
		if err != nil {
			return tg.OutMessage{}, err
		}
		v.Members = ms
		for _, u := range ms {
			if u.ID == d.Data.AssigneeID {
				v.AssigneeName = u.Username
			}
		}
	}
	return ui.WizardCard(v, r.Prefs.Locale, tzOf(r), h.Clock.Now()), nil
}

// draw edits the card to d's step. A failure is only logged.
func (h *Handler) draw(ctx context.Context, r req, d model.Draft, o cardOpts) {
	out, err := h.cardMessage(ctx, r, d, o)
	if err != nil {
		h.Log.Error("wizard card", "chat", d.ChatID, "err", err)
		return
	}
	h.logErr("edit wizard card", h.Send.Edit(ctx, d.ChatID, d.MsgID, out))
}

// redrawStored redraws the card from the stored draft (after a stale press).
// found=false means the draft is gone: nothing is drawn. The card is left
// as it is, because a missing draft usually means the wizard just finished
// (Save or Cancel on this or another node) and the card shows the result;
// overwriting it with "expired" would make the user think it failed. This
// deviates from §12 ("the card's keyboard is removed") on purpose: the
// handler cannot tell a finished draft from an expired one, so it answers
// with a toast only.
func (h *Handler) redrawStored(ctx context.Context, r req, chatID int64, msgID int) (found bool) {
	d, err := h.Svc.DraftByMessage(ctx, chatID, msgID)
	if err != nil || !knownStep(d.Step) {
		if err != nil && !errors.Is(err, service.ErrNotFound) {
			h.Log.Error("wizard draft", "chat", chatID, "err", err)
			return true
		}
		return false
	}
	h.draw(ctx, r, d, cardOpts{})
	return true
}

// staleToast redraws the current step and returns the toast for the press.
func (h *Handler) staleToast(ctx context.Context, r req, chatID int64, msgID int) string {
	lc := r.Prefs.Locale
	if h.redrawStored(ctx, r, chatID, msgID) {
		return lc.T("wizard.toast.stale")
	}
	return lc.T("wizard.toast.expired")
}

// commit stores nd (a changed copy of old) and draws it. stale=true means
// the draft changed meanwhile (another press or node, or it expired): nothing
// was stored. A view (o.view) is drawn without a write.
func (h *Handler) commit(ctx context.Context, r req, old, nd model.Draft, o cardOpts) (stale bool, err error) {
	if o.view {
		h.draw(ctx, r, old, o)
		return false, nil
	}
	if r.Actor.ChatKind == model.ChatGroup && nd.Step != old.Step &&
		(nd.Step == model.StepText || nd.Step == model.StepTimeText) {
		if nd.AskMsgID, err = h.sendAsk(ctx, r, nd); err != nil {
			return false, err
		}
	}
	nd.Data.Cal = string(calOf(r.Prefs.Locale)) // the card is drawn in this calendar
	if err := h.Svc.UpdateDraft(ctx, &nd); err != nil {
		if errors.Is(err, service.ErrStaleDraft) {
			return true, nil
		}
		return false, err
	}
	h.draw(ctx, r, nd, o)
	return false, nil
}

// ---- button presses ----

// wizardCallback handles every wizard button. It returns the toast for the
// callback answer.
func (h *Handler) wizardCallback(ctx context.Context, cb *tg.Callback, r req, act ui.Action) (string, error) {
	lc := r.Prefs.Locale
	switch act.Kind {
	case ui.ActWizStart:
		return "", h.startWizard(ctx, r, cb.ChatID, 0)
	case ui.ActWizNoop:
		if act.Arg == ui.WizNoopPast {
			return lc.T("wizard.toast.past"), nil
		}
		return "", nil
	}
	d, err := h.Svc.DraftByMessage(ctx, cb.ChatID, cb.MessageID)
	if errors.Is(err, service.ErrNotFound) || (err == nil && !knownStep(d.Step)) {
		return lc.T("wizard.toast.expired"), nil // toast only: see redrawStored
	}
	if err != nil {
		return "", err
	}
	if d.UserID != cb.FromID {
		return lc.T("wizard.toast.foreign"), nil
	}
	if act.ID != d.Version {
		return h.staleToast(ctx, r, cb.ChatID, cb.MessageID), nil
	}

	nd, o, toast, err := h.press(ctx, r, d, act)
	if err != nil {
		return "", err
	}
	if o.done {
		return toast, nil
	}
	stale, err := h.commit(ctx, r, d, nd, o)
	if err != nil {
		return "", err
	}
	if stale {
		return h.staleToast(ctx, r, cb.ChatID, cb.MessageID), nil
	}
	return toast, nil
}

// press applies act to a copy of d.
func (h *Handler) press(ctx context.Context, r req, d model.Draft, act ui.Action) (model.Draft, cardOpts, string, error) {
	var (
		nd    = d
		o     cardOpts
		toast string
		err   error
	)
	switch act.Kind {
	case ui.ActWizNav:
		return h.pressNav(ctx, r, d, act.Arg)
	case ui.ActWizDate:
		nd, o, toast, err = h.pressDate(ctx, r, d, act.Arg)
	case ui.ActWizMonth:
		o = h.pressMonth(r, d, act.Arg)
	case ui.ActWizTime:
		nd, o = pressTime(ctx, r, d, act.Arg)
	case ui.ActWizRepeat:
		nd, o, err = h.pressRepeat(ctx, r, d, act.Arg)
	case ui.ActWizAssign:
		nd, o, err = h.pressAssign(ctx, r, d, act.Arg)
	case ui.ActWizEdit:
		nd, o = pressEdit(d, act.Arg)
	default:
		o.view = true
	}
	return nd, o, toast, err
}

func (h *Handler) pressNav(ctx context.Context, r req, d model.Draft, arg string) (model.Draft, cardOpts, string, error) {
	lc := r.Prefs.Locale
	o := cardOpts{}
	switch arg {
	case ui.WizNavCancel:
		if err := h.Svc.EndDraft(ctx, d); err != nil {
			if errors.Is(err, service.ErrStaleDraft) {
				return d, cardOpts{done: true}, h.staleToast(ctx, r, d.ChatID, d.MsgID), nil
			}
			return d, o, "", err
		}
		h.logErr("edit cancelled card", h.Send.Edit(ctx, d.ChatID, d.MsgID, tg.OutMessage{Text: lc.T("wizard.cancelled")}))
		return d, cardOpts{done: true}, "", nil
	case ui.WizNavSave:
		if d.Step != model.StepConfirm {
			o.view = true
			return d, o, "", nil
		}
		toast, err := h.save(ctx, r, d)
		return d, cardOpts{done: true}, toast, err
	case ui.WizNavEdit:
		o.view = true
		if d.Step == model.StepConfirm {
			o.EditMenu = true
		}
		return d, o, "", nil
	case ui.WizNavUseFound, ui.WizNavNoFound:
		if d.Step != model.StepFound {
			o.view = true
			return d, o, "", nil
		}
		nd, fo, err := h.pressFound(ctx, r, d, arg == ui.WizNavUseFound)
		return nd, fo, "", err
	case ui.WizNavBack:
		nd, bo := back(d)
		return nd, bo, "", nil
	case ui.WizNavSkip:
		return h.pressSkip(ctx, r, d)
	}
	o.view = true
	return d, o, "", nil
}

func (h *Handler) pressSkip(ctx context.Context, r req, d model.Draft) (model.Draft, cardOpts, string, error) {
	switch d.Step {
	case model.StepDate:
		return h.pressDate(ctx, r, d, ui.WizDateNone)
	case model.StepTime:
		nd, o := pressTime(ctx, r, d, ui.WizTimeDefault)
		return nd, o, "", nil
	case model.StepRepeat:
		nd, o, err := h.pressRepeat(ctx, r, d, ui.WizRepeatOnce)
		return nd, o, "", err
	case model.StepAssignee:
		arg := ui.WizAssignMe
		if r.Actor.ChatKind == model.ChatGroup {
			arg = ui.WizAssignNone
		}
		nd, o, err := h.pressAssign(ctx, r, d, arg)
		return nd, o, "", err
	}
	return d, cardOpts{view: true}, "", nil
}

// goConfirm moves to the summary and ends editing.
func goConfirm(d *model.Draft) {
	d.Step = model.StepConfirm
	d.Data.Editing = false
}

// afterAnswer goes to confirm when an edit is under way, otherwise to next.
func afterAnswer(d *model.Draft, next model.DraftStep) {
	if d.Data.Editing {
		goConfirm(d)
		return
	}
	d.Step = next
}

// toAssignee goes to the assignee step, or past it when a private chat has
// no members (Me is then the only choice and is stored).
func (h *Handler) toAssignee(ctx context.Context, r req, d *model.Draft) error {
	if d.Data.Editing {
		goConfirm(d)
		return nil
	}
	if r.Actor.ChatKind == model.ChatGroup {
		d.Step = model.StepAssignee
		return nil
	}
	ms, err := h.members(ctx, r.Actor.UserID)
	if err != nil {
		return err
	}
	if len(ms) == 0 {
		d.Data.AssigneeID = r.Actor.UserID
		goConfirm(d)
		return nil
	}
	d.Step = model.StepAssignee
	return nil
}

// back goes to the previous step that was shown, from the data.
func back(d model.Draft) (model.Draft, cardOpts) {
	nd := d
	if d.Data.Editing && d.Step != model.StepTimeText {
		goConfirm(&nd)
		return nd, cardOpts{}
	}
	switch d.Step {
	case model.StepFound, model.StepDate:
		nd.Step = model.StepText
	case model.StepTime:
		nd.Step = model.StepDate
	case model.StepTimeText, model.StepRepeat:
		nd.Step = model.StepTime
	case model.StepAssignee:
		switch {
		case d.Data.Date != "":
			nd.Step = model.StepRepeat
		case hasFound(d):
			nd.Step = model.StepFound
		default:
			nd.Step = model.StepDate
		}
	default: // text, confirm: nothing before them
		return d, cardOpts{view: true}
	}
	return nd, cardOpts{}
}

func (h *Handler) pressFound(ctx context.Context, r req, d model.Draft, use bool) (model.Draft, cardOpts, error) {
	nd := d
	if !use {
		nd.Data.FoundText, nd.Data.FoundDue, nd.Data.FoundRule, nd.Data.FoundTime = "", nil, "", false
		nd.Data.Text = d.Data.OrigText
		nd.Step = model.StepDate
		return nd, cardOpts{}, nil
	}
	nd.Data.Text = d.Data.FoundText
	nd.Data.Date, nd.Data.Time, nd.Data.Repeat = "", "", ""
	err := h.toAssignee(ctx, r, &nd)
	return nd, cardOpts{}, err
}

// repeatKind maps a canonical rule string to the wizard's repeat choice
// ("" = once, or a rule the wizard cannot express).
func repeatKind(rule string) string {
	kind, rest, _ := strings.Cut(rule, " ")
	if _, nth, ok := strings.Cut(rest, "#"); ok && kind == "monthly" {
		if strings.HasPrefix(nth, "last") {
			return model.RepeatLastWeekday
		}
		return model.RepeatNthWeekday
	}
	switch kind {
	case "daily":
		return model.RepeatDaily
	case "weekly":
		return model.RepeatWeekly
	case "monthly":
		return model.RepeatMonthly
	case "yearly":
		return model.RepeatYearly
	}
	return model.RepeatOnce
}

// nthRepeatValid says whether repeat (RepeatNthWeekday or RepeatLastWeekday)
// is one of the choices offered for the date "YYYY-MM-DD" in calendar cal.
func nthRepeatValid(repeat, date string, cal calendar.Kind) bool {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	_, hasNth, hasLast := ui.NthWeekdayChoices(cal, t)
	switch repeat {
	case model.RepeatNthWeekday:
		return hasNth
	case model.RepeatLastWeekday:
		return hasLast
	}
	return false
}

// nthRepeatKept says whether the draft's "Nth/last weekday" repeat may stay
// when the date changes to newDate: only when the weekday is the same and
// the choice is still offered. Any other repeat is kept.
func nthRepeatKept(old model.DraftData, newDate string, cal calendar.Kind) bool {
	if old.Repeat != model.RepeatNthWeekday && old.Repeat != model.RepeatLastWeekday {
		return true
	}
	o, err1 := time.Parse("2006-01-02", old.Date)
	n, err2 := time.Parse("2006-01-02", newDate)
	if err1 != nil || err2 != nil || o.Weekday() != n.Weekday() {
		return false
	}
	return nthRepeatValid(old.Repeat, newDate, cal)
}

// clearFound drops the found date: an explicit date choice replaces it.
func clearFound(d *model.Draft) {
	d.Data.FoundText, d.Data.FoundDue, d.Data.FoundRule, d.Data.FoundTime = "", nil, "", false
}

func (h *Handler) pressDate(ctx context.Context, r req, d model.Draft, arg string) (model.Draft, cardOpts, string, error) {
	view := cardOpts{view: true}
	ignore := cardOpts{done: true} // rejected: only the callback answer
	if d.Step != model.StepDate {
		return d, view, "", nil
	}
	lc := r.Prefs.Locale
	loc := tzOf(r)
	now := h.Clock.Now().In(loc)
	nd := d
	switch arg {
	case ui.WizDateQuick:
		return d, view, "", nil
	case ui.WizDateGrid:
		cur := calOf(lc).Split(now)
		cur.Day = 1
		view.Grid = &cur
		return d, view, "", nil
	case ui.WizDateNone:
		nd.Data.Date, nd.Data.Time, nd.Data.Repeat = "", "", ""
		clearFound(&nd)
		err := h.toAssignee(ctx, r, &nd)
		return nd, cardOpts{}, "", err
	}
	date, ok := ui.ParseDateArg(arg)
	if !ok {
		return d, ignore, "", nil
	}
	ty, tm, td := now.Date()
	switch key := dateKey(date.Year, time.Month(date.Month), date.Day); {
	case key < dateKey(ty, tm, td):
		return d, ignore, lc.T("wizard.toast.past"), nil
	case key > dateKey(ty+wizardWindowYears, tm, td):
		return d, ignore, "", nil
	}
	nd.Data.Date = date.String()
	old := d.Data
	if hasFound(d) {
		// The found rule is dropped for an explicit date: offer its kind as
		// the current repeat choice, so it is not lost silently. A found
		// "monthly fri#last" is judged like an earlier choice on its weekday.
		nd.Data.Repeat = repeatKind(d.Data.FoundRule)
		if rule, err := recur.Parse(d.Data.FoundRule); err == nil && len(rule.Weekdays) == 1 {
			old.Date = time.Date(2000, 1, 2+int(rule.Weekdays[0]), 0, 0, 0, 0, time.UTC).Format("2006-01-02") // 2000-01-02 is a Sunday
		}
		old.Repeat = nd.Data.Repeat
	}
	if !nthRepeatKept(old, nd.Data.Date, calOf(lc)) {
		nd.Data.Repeat = model.RepeatOnce // "Nth Friday" must not outlive its date
	}
	clearFound(&nd)
	// A new date walks on through time and repeat, even during an edit.
	nd.Data.Editing = false
	nd.Step = model.StepTime
	return nd, cardOpts{}, "", nil
}

// pressMonth shows another month of the grid. The month must be in the
// user's calendar, from the current month to five years ahead.
func (h *Handler) pressMonth(r req, d model.Draft, arg string) cardOpts {
	view := cardOpts{done: true} // rejected: only the callback answer
	cal, y, m, ok := ui.ParseMonthArg(arg)
	lc := r.Prefs.Locale
	if !ok || d.Step != model.StepDate || cal != calOf(lc) || m < 1 || m > 12 {
		return view
	}
	cur := cal.Split(h.Clock.Now().In(tzOf(r)))
	idx := func(y, m int) int { return y*12 + m - 1 }
	if i := idx(y, m); i < idx(cur.Year, cur.Month) || i > idx(cur.Year+wizardWindowYears, cur.Month) {
		return view
	}
	return cardOpts{view: true, Grid: &calendar.Date{Year: y, Month: m, Day: 1}}
}

func pressTime(_ context.Context, _ req, d model.Draft, arg string) (model.Draft, cardOpts) {
	if d.Step != model.StepTime {
		return d, cardOpts{view: true}
	}
	nd := d
	switch arg {
	case ui.WizTimeType:
		nd.Step = model.StepTimeText
		return nd, cardOpts{}
	case ui.WizTimeDefault:
		nd.Data.Time = ""
	default:
		hh, mm, ok := ui.ParseTimeArg(arg)
		if !ok {
			return d, cardOpts{view: true}
		}
		nd.Data.Time = fmt.Sprintf("%02d:%02d", hh, mm)
	}
	afterAnswer(&nd, model.StepRepeat)
	return nd, cardOpts{}
}

func (h *Handler) pressRepeat(ctx context.Context, r req, d model.Draft, arg string) (model.Draft, cardOpts, error) {
	if d.Step != model.StepRepeat {
		return d, cardOpts{view: true}, nil
	}
	nd := d
	switch arg {
	case ui.WizRepeatOnce:
		nd.Data.Repeat = model.RepeatOnce
	case ui.WizRepeatDaily:
		nd.Data.Repeat = model.RepeatDaily
	case ui.WizRepeatWeekly:
		nd.Data.Repeat = model.RepeatWeekly
	case ui.WizRepeatMonthly:
		nd.Data.Repeat = model.RepeatMonthly
	case ui.WizRepeatYearly:
		nd.Data.Repeat = model.RepeatYearly
	case ui.WizRepeatNth, ui.WizRepeatLast:
		want := model.RepeatNthWeekday
		if arg == ui.WizRepeatLast {
			want = model.RepeatLastWeekday
		}
		// Only a choice that the card offered for this date is taken.
		if !nthRepeatValid(want, d.Data.Date, calOf(r.Prefs.Locale)) {
			return d, cardOpts{view: true}, nil
		}
		nd.Data.Repeat = want
	default:
		return d, cardOpts{view: true}, nil
	}
	err := h.toAssignee(ctx, r, &nd)
	return nd, cardOpts{}, err
}

func (h *Handler) pressAssign(ctx context.Context, r req, d model.Draft, arg string) (model.Draft, cardOpts, error) {
	view := cardOpts{view: true}
	if d.Step != model.StepAssignee {
		return d, view, nil
	}
	var id int64
	switch arg {
	case ui.WizAssignNone:
		if r.Actor.ChatKind != model.ChatGroup {
			return d, view, nil
		}
	case ui.WizAssignMe:
		id = r.Actor.UserID
	default:
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil || n <= 0 || !h.Allowed[n] {
			return d, view, nil
		}
		ms, err := h.members(ctx, r.Actor.UserID)
		if err != nil {
			return d, view, err
		}
		found := false
		for _, u := range ms {
			found = found || u.ID == n
		}
		if !found {
			return d, view, nil
		}
		id = n
	}
	nd := d
	nd.Data.AssigneeID = id
	goConfirm(&nd)
	return nd, cardOpts{}, nil
}

// pressEdit opens the chosen step from the edit menu. Time and repeat
// need a date: without one the date step comes first. A found date is
// replaced by an explicit one, so editing it also goes to the date step.
func pressEdit(d model.Draft, arg string) (model.Draft, cardOpts) {
	if d.Step != model.StepConfirm {
		return d, cardOpts{view: true}
	}
	nd := d
	nd.Data.Editing = true
	switch arg {
	case ui.WizEditText:
		nd.Step = model.StepText
	case ui.WizEditDate:
		nd.Step = model.StepDate
	case ui.WizEditTime:
		nd.Step = model.StepTime
		if d.Data.Date == "" {
			nd.Step = model.StepDate
		}
	case ui.WizEditRepeat:
		nd.Step = model.StepRepeat
		if d.Data.Date == "" {
			nd.Step = model.StepDate
		}
	case ui.WizEditAssignee:
		nd.Step = model.StepAssignee
	default:
		return d, cardOpts{view: true}
	}
	return nd, cardOpts{}
}

// ---- text answers ----

var typedTime = regexp.MustCompile(`^(\d{1,2})(?:[:.](\d{2}))?\s*([ap]m)?$`)

// parseTypedTime reads "15:30", "3pm", "3:30 pm", "15" or Persian digits.
func parseTypedTime(s string) (hh, mm int, ok bool) {
	m := typedTime.FindStringSubmatch(strings.ToLower(strings.TrimSpace(i18n.NormalizeDigits(s))))
	if m == nil {
		return 0, 0, false
	}
	hh, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		mm, _ = strconv.Atoi(m[2])
	}
	switch m[3] {
	case "":
		if hh > 23 {
			return 0, 0, false
		}
	default:
		if hh < 1 || hh > 12 {
			return 0, 0, false
		}
		hh %= 12
		if m[3] == "pm" {
			hh += 12
		}
	}
	return hh, mm, mm <= 59
}

// answerText applies a text answer to a copy of d. An answer that cannot be
// used leaves the step as it is and sets a note (a view: nothing is stored).
func (h *Handler) answerText(r req, d model.Draft, text string) (model.Draft, cardOpts, error) {
	lc := r.Prefs.Locale
	nd := d
	note := func(key string, args ...any) (model.Draft, cardOpts, error) {
		return d, cardOpts{view: true, Note: lc.T(key, args...)}, nil
	}
	if d.Step == model.StepTimeText {
		hh, mm, ok := parseTypedTime(text)
		if !ok {
			return note("wizard.note.badtime")
		}
		nd.Data.Time = fmt.Sprintf("%02d:%02d", hh, mm)
		afterAnswer(&nd, model.StepRepeat)
		return nd, cardOpts{}, nil
	}
	norm := strings.TrimSpace(i18n.NormalizeDigits(text))
	if norm == "" {
		return note("wizard.note.empty")
	}
	if tooLong(norm) {
		return note("wizard.note.toolong", lc.Num(len([]rune(norm))), lc.Num(maxTodoChars))
	}
	nd.Data.Text, nd.Data.OrigText = norm, norm
	if d.Data.Editing {
		// A text edit does not detect a date again.
		goConfirm(&nd)
		return nd, cardOpts{}, nil
	}
	clearFound(&nd)
	nd.Step = model.StepDate
	res, err := h.Parser.Parse(norm, h.Clock.Now().In(tzOf(r)), calOf(lc))
	if err == nil && res.Due != nil && strings.TrimSpace(res.Text) != "" {
		due := res.Due.UTC()
		nd.Data.FoundText, nd.Data.FoundDue, nd.Data.FoundTime = res.Text, &due, res.HasTime
		if res.Rule != nil {
			nd.Data.FoundRule = res.Rule.String()
		}
		nd.Step = model.StepFound
	}
	return nd, cardOpts{}, nil
}
