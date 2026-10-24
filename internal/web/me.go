package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

type meUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Lang     string `json:"lang"`
	Calendar string `json:"calendar"`
	TZ       string `json:"tz"`
	Dir      string `json:"dir"`
}

type meSession struct {
	Kind      string    `json:"kind"`
	ExpiresAt time.Time `json:"expires_at"`
}

type meBot struct {
	Username string `json:"username"`
}

type meResponse struct {
	User    meUser    `json:"user"`
	Session meSession `json:"session"`
	Bot     meBot     `json:"bot"`
}

// dirOf is the text direction of a language.
func dirOf(l i18n.Lang) string {
	if l == i18n.Fa {
		return "rtl"
	}
	return "ltr"
}

// meOf builds the body of GET /api/me for the session of r. W8 can use it
// for the answer of PUT /api/me/settings.
func (s *Server) meOf(r *http.Request) (meResponse, error) {
	sess := sessionFrom(r)
	username := ""
	u, err := s.d.Web.User(r.Context(), sess.UserID)
	switch {
	case err == nil:
		username = u.Username
	case !errors.Is(err, service.ErrNotFound):
		return meResponse{}, err
	}
	p := sess.prefs
	tz := "UTC"
	if p.TZ != nil {
		tz = p.TZ.String()
	}
	return meResponse{
		User: meUser{ID: sess.UserID, Username: username, Lang: string(p.Locale.Lang),
			Calendar: string(p.Locale.Cal), TZ: tz, Dir: dirOf(p.Locale.Lang)},
		Session: meSession{Kind: string(sess.Kind), ExpiresAt: sess.ExpiresAt.UTC()},
		Bot:     meBot{Username: s.d.BotName},
	}, nil
}

// handleMe is endpoint 7, GET /api/me.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	me, err := s.meOf(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}
