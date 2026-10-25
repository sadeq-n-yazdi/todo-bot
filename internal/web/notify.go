package web

import (
	"context"
	"net/http"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// didEchoRunes is how much of the item text the "did" notice repeats, as
// the confirmations of the bot do.
const didEchoRunes = 200

// shouldNotifyDid tells if a Done by a in the web needs the "did" notice
// (docs/WEB-DESIGN.md §5.4, ARCHITECTURE §11): the actor is the assignee of
// the item and acts from another chat than the chat of the item.
func shouldNotifyDid(a service.Actor, done model.Todo) bool {
	return done.AssigneeID != 0 && done.AssigneeID == a.UserID && done.ChatID != a.ChatID
}

// notifyDid tells the chat of the item that its assignee completed it. The
// text is in the locale of the creator of the item. The caller checks
// shouldNotifyDid and that the Done changed the item.
//
// The handler reads the prefs and builds the text. The send runs in a
// goroutine, as the sign-in notice does, so a slow Telegram does not hold
// the response; s.codes.notices counts it, and tests wait on it. A send
// error gives only a warning in the log: the item is already done.
func (s *Server) notifyDid(r *http.Request, done model.Todo) {
	base := context.WithoutCancel(r.Context())
	lc := i18n.Default("")
	if p, err := s.d.Svc.Prefs(base, done.CreatedBy, ""); err != nil {
		s.log.Warn("web: did notice prefs", "user", done.CreatedBy, "err", err)
	} else {
		lc = p.Locale
	}
	chatID := done.ChatID
	msg := tg.OutMessage{Text: lc.T("web.tg.did", ui.Mention(done.AssigneeID, done.AssigneeName, lc), echoText(done.Text))}
	s.codes.notices.Add(1)
	go func() {
		defer s.codes.notices.Done()
		ctx, cancel := context.WithTimeout(base, noticeTimeout)
		defer cancel()
		if _, err := s.d.Send.Send(ctx, chatID, msg); err != nil {
			s.log.Warn("web: did notice", "chat", chatID, "err", err)
		}
	}()
}

// echoText shortens text to didEchoRunes runes (plus an ellipsis) and
// escapes it for Telegram HTML. It cuts before it escapes, because the
// escape can make the text longer.
func echoText(text string) string {
	if r := []rune(text); len(r) > didEchoRunes {
		text = string(r[:didEchoRunes]) + "…"
	}
	return ui.Escape(text)
}
