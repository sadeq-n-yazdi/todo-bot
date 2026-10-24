package web

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Limits of the Mini App sign-in (docs/WEB-DESIGN.md §3.2, §3.5, §9.1).
const (
	// signInAge is the age limit of initData for a sign-in.
	signInAge = time.Hour
	// miniAppTTL is the idle and the absolute limit of a miniapp session.
	miniAppTTL = 12 * time.Hour
)

// startParamRE is the start_param that the app can use: the code screen,
// or the tab of one group. Any other value is dropped, so that the app
// never acts on text from a link.
var startParamRE = regexp.MustCompile(`^(code|g-?[0-9]{1,19})$`)

// initDataError maps an error of verifyInitData to the API error.
func initDataError(err error) error {
	switch {
	case errors.Is(err, errInitBadSignature):
		return errBadSignature
	case errors.Is(err, errInitExpired):
		return errExpired
	case errors.Is(err, errInitBadRequest), errors.Is(err, errInitBadUser):
		return errBadRequest
	}
	return err
}

type miniAppUser struct {
	ID int64 `json:"id"`
}

type miniAppResponse struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      miniAppUser `json:"user"`
	Start     string      `json:"start"`
}

// handleMiniAppSignIn is endpoint 4, POST /api/auth/miniapp. verifyInitData
// does steps 1 to 9 of §3.2; this handler does steps 10 to 13. Neither the
// initData nor the token goes to the log.
func (s *Server) handleMiniAppSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InitData string `json:"init_data"`
	}
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := verifyInitData(req.InitData, s.d.Token, s.now(), signInAge)
	if err != nil {
		s.fail(w, r, initDataError(err))
		return
	}
	ri := infoOf(r)
	ri.loc = i18n.Default(d.LanguageCode)
	// 10. The allowlist. The body gives the user ID, for the owner.
	if !s.d.Allowed[d.UserID] {
		s.fail(w, r, errNotAllowed.with("user_id", d.UserID))
		return
	}
	// 11. Prefs creates the user on first contact, as the bot does.
	prefs, err := s.d.Svc.Prefs(r.Context(), d.UserID, d.LanguageCode)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ri.loc = prefs.Locale
	// 12. A failure here is only a warning.
	if err := s.d.Svc.NoteUsername(r.Context(), d.UserID, d.Username); err != nil {
		s.log.Warn("web: note username", "user", d.UserID, "err", err)
	}
	// 13. The session.
	token, ws, err := s.d.Web.StartWebSession(r.Context(), d.UserID, model.WebMiniApp, r.UserAgent(), miniAppTTL, miniAppTTL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ri.sess = &session{WebSession: ws, prefs: prefs} // for the log line: the user ID only
	start := ""
	if startParamRE.MatchString(d.StartParam) {
		start = d.StartParam
	}
	writeJSON(w, http.StatusOK, miniAppResponse{
		Token: token, ExpiresAt: ws.ExpiresAt.UTC(), User: miniAppUser{ID: d.UserID}, Start: start,
	})
}

// handleLogout is endpoint 5: it deletes this session and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Web.EndWebSession(r.Context(), sessionFrom(r).token); err != nil {
		s.fail(w, r, err)
		return
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleLogoutAll is endpoint 6: it deletes every session of the user.
func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Web.EndUserWebSessions(r.Context(), sessionFrom(r).UserID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
