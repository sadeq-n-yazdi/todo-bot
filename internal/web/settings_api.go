package web

import (
	"net/http"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// settingsRequest is the body of endpoint 21. A field that is not in the
// request does not change.
type settingsRequest struct {
	Lang     *string `json:"lang"`
	Calendar *string `json:"calendar"`
}

// handleSettings is endpoint 21, PUT /api/me/settings. It changes the
// language, the calendar or both, and answers as GET /api/me with the new
// values. It checks both values before it writes one of them. After a change
// of language it calls SetKBVersion(user, 0), as the bot does, so the bot
// sends its keyboard again in the new language (§5.4).
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Lang == nil && req.Calendar == nil {
		s.fail(w, r, errBadRequest)
		return
	}
	var lang i18n.Lang
	if req.Lang != nil {
		if lang = i18n.Lang(*req.Lang); !lang.Valid() {
			s.fail(w, r, errBadLangInput)
			return
		}
	}
	var cal calendar.Kind
	if req.Calendar != nil {
		if cal = calendar.Kind(*req.Calendar); !cal.Valid() {
			s.fail(w, r, errBadCalendar)
			return
		}
	}
	ctx := r.Context()
	sess := sessionFrom(r)
	uid := sess.UserID
	if req.Lang != nil && lang != sess.prefs.Locale.Lang {
		if err := s.d.Svc.SetLang(ctx, uid, lang); err != nil {
			s.fail(w, r, err)
			return
		}
		if err := s.d.Svc.SetKBVersion(ctx, uid, 0); err != nil {
			s.log.Warn("web: reset keyboard version", "user", uid, "err", err)
		}
	}
	if req.Calendar != nil && cal != sess.prefs.Locale.Cal {
		if err := s.d.Svc.SetCalendar(ctx, uid, cal); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	// The session holds the preferences of the start of the request: read
	// them again, so the answer and its error texts use the new values.
	prefs, err := s.d.Svc.Prefs(ctx, uid, "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sess.prefs = prefs
	infoOf(r).loc = prefs.Locale
	me, err := s.meOf(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}
