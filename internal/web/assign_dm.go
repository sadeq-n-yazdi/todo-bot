package web

import (
	"context"
	"net/http"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// The message to a new assignee after an edit in the web (endpoint 23,
// docs/WEB-DESIGN.md §5.4). The rules are the rules of #98 §4 (decision 3).

// shouldNotifyAssigned tells if an edit by editor that gave the item after
// to its assignee needs the message: the assignee changed from old, the new
// assignee is a user, and it is not the editor. after comes from
// UpdateTodo, so its AssigneeID is already normalised (the own private chat
// of the assignee is 0).
func shouldNotifyAssigned(old int64, after model.Todo, editor int64) bool {
	id := after.AssigneeID
	return id != old && id != 0 && id != editor
}

// notifyAssigned sends one private message to the new assignee of after.
// The text is in the language, calendar and time zone of the assignee, and
// has no buttons: the reminders bring the buttons later. There is no retry.
//
// The send is synchronous, because the response tells the editor when the
// assignee cannot get the message. It uses a context without the cancel of
// the request, so a client that goes away after the save does not stop it.
//
// It returns a note in plain text for the editor when Telegram refuses the
// message for good (the assignee never started the bot, or blocked it).
// Any other error gives only a warning in the log and no note. The item is
// already saved in both cases.
func (s *Server) notifyAssigned(r *http.Request, after model.Todo) string {
	base := context.WithoutCancel(r.Context())
	ctx, cancel := context.WithTimeout(base, noticeTimeout)
	defer cancel()
	editor := sessionFrom(r).UserID
	id := after.AssigneeID

	v := view{lc: i18n.Default(""), loc: s.d.Config.DefaultTZ, now: s.d.Clock.Now()}
	if p, err := s.d.Svc.Prefs(ctx, id, ""); err != nil {
		s.log.Warn("web: assigned dm prefs", "user", id, "err", err)
	} else {
		v.lc = p.Locale
		if p.TZ != nil {
			v.loc = p.TZ
		}
	}
	if v.loc == nil {
		v.loc = time.UTC
	}
	lc := v.lc
	name := ""
	if u, err := s.d.Web.User(ctx, editor); err == nil {
		name = u.Username
	}
	text := lc.T("web.tg.assigned", ui.Mention(editor, name, lc), assignedSummary(after, v))
	_, err := s.d.Send.Send(ctx, id, tg.OutMessage{Text: text})
	switch {
	case err == nil:
		return ""
	case tg.IsPermanent(err):
		s.log.Info("web: assigned dm not delivered", "user", id, "err", err)
		elc := s.viewOf(r).lc
		who := elc.T("web.item.noname")
		if after.AssigneeName != "" {
			who = "@" + after.AssigneeName
		}
		return elc.T("web.edit.nodm", who)
	default:
		s.log.Warn("web: assigned dm", "user", id, "err", err)
		return ""
	}
}

// assignedSummary is the item part of the message, in Telegram HTML: the
// text, the date and the repeat, as the bot shows an item. It has no
// "→ @assignee", because the message goes to the assignee.
func assignedSummary(t model.Todo, v view) string {
	s := echoText(t.Text)
	if t.DueAt != nil {
		s += " — " + ui.Escape(ui.FormatWhen(*t.DueAt, v.lc, v.loc, v.now))
	}
	if t.Recur != "" {
		s += " 🔁"
		if d := ui.DescribeRule(t.Recur, v.lc); d != "" {
			s += " " + ui.Escape(d)
		}
	}
	return s
}
