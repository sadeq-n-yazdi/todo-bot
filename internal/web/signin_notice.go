package web

import (
	"context"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// noticeTimeout is the time that the sign-in notice can use.
const noticeTimeout = 5 * time.Second

// sendSignInNotice sends the sign-in notice of docs/WEB-DESIGN.md §3.3.1 to
// the private chat of the user (its chat ID is the user ID). It follows each
// new browser session; the login code and, later, the Login Widget call it.
// The text holds the country and the client IP only, never a value that the
// browser chose. The button opens the sign-out screen of the Mini App.
//
// The send runs in a goroutine, so a slow Telegram does not hold the response.
// A send error gives only a warning in the log. The session stays valid.
func (s *Server) sendSignInNotice(r *http.Request, userID int64, loc i18n.Locale) {
	// Read everything from the request now: it ends when the handler returns.
	text := loc.T("web.tg.signin", html.EscapeString(s.clientCountry(r)), html.EscapeString(s.clientIP(r).String()))
	msg := tg.OutMessage{
		Text: text,
		Keyboard: [][]tg.Button{{{
			Text:      loc.T("web.tg.signin_logout"),
			WebAppURL: strings.TrimRight(s.d.Config.WebOrigin, "/") + "/tg/?a=signout",
		}}},
	}
	base := context.WithoutCancel(r.Context())
	s.codes.notices.Add(1)
	go func() {
		defer s.codes.notices.Done()
		ctx, cancel := context.WithTimeout(base, noticeTimeout)
		defer cancel()
		if _, err := s.d.Send.Send(ctx, userID, msg); err != nil {
			s.log.Warn("web: sign-in notice", "user", userID, "err", err)
		}
	}()
}
