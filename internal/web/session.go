package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// cookieName is the session cookie of the browser and the PWA (§3.5). The
// __Host- prefix makes the browser refuse it unless it is Secure, has
// Path=/ and has no Domain.
const cookieName = "__Host-tb_session"

// session is the live session of a request, with the user's preferences.
type session struct {
	model.WebSession
	token string // never logged
	prefs model.Prefs
}

// sessionFrom returns the session of the request. It is nil outside the
// routes with a session.
func sessionFrom(r *http.Request) *session {
	if ri := infoOf(r); ri != nil {
		return ri.sess
	}
	return nil
}

// requestToken reads the token: the bearer header first, else the cookie
// (§3.5, "the header wins").
func requestToken(r *http.Request) (token string, fromCookie bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(t), false
		}
		return "", false
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value, true
	}
	return "", false
}

// withSession is the check of §3.5 on each request:
//  1. read the token (a token of the wrong form is not found by the service);
//  2. find the live session, or answer 401 unauthenticated;
//  3. if the user is not in the allowlist, delete every session of the
//     user and answer 401 session_revoked;
//  4. the service slides a browser session at most once per hour; then the
//     cookie goes out again with the new Max-Age.
//
// It also reads the user's preferences for the locale of the response.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, fromCookie := requestToken(r)
		if token == "" {
			s.fail(w, r, errUnauthn)
			return
		}
		ws, err := s.d.Web.WebSession(r.Context(), token, s.d.Config.WebSessionTTL)
		if err != nil {
			if fromCookie && errors.Is(err, service.ErrNotFound) {
				s.clearCookie(w)
			}
			if errors.Is(err, service.ErrNotFound) {
				err = errUnauthn
			}
			s.fail(w, r, err)
			return
		}
		if !s.d.Allowed[ws.UserID] {
			if err := s.d.Web.EndUserWebSessions(r.Context(), ws.UserID); err != nil {
				s.fail(w, r, err)
				return
			}
			if fromCookie {
				s.clearCookie(w)
			}
			s.fail(w, r, errRevoked)
			return
		}
		// The service slid the session in this request: seen_at is now and
		// later than the start (the start already sent its cookie).
		if fromCookie && ws.Kind == model.WebBrowser && ws.SeenAt.After(ws.CreatedAt) &&
			!ws.SeenAt.Before(s.now().UTC().Truncate(time.Second)) {
			s.setCookie(w, token, ws.ExpiresAt)
		}
		prefs, err := s.d.Svc.Prefs(r.Context(), ws.UserID, "")
		if err != nil {
			s.fail(w, r, err)
			return
		}
		ri := infoOf(r)
		ri.sess = &session{WebSession: ws, token: token, prefs: prefs}
		ri.loc = prefs.Locale
		next.ServeHTTP(w, r)
	})
}

// userLimit applies a limit per user. It runs after withSession.
func (s *Server) userLimit(l *limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := l.allow(strconv.FormatInt(sessionFrom(r).UserID, 10)); !ok {
			s.fail(w, r, errRateLimited.retry(retry))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// miniAppOnly refuses a browser session on an "MA" route with 403
// wrong_session. It runs after withSession.
func (s *Server) miniAppOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionFrom(r).Kind != model.WebMiniApp {
			s.fail(w, r, errWrongSession)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setCookie sends the session cookie of a browser session that expires at
// expires.
func (s *Server) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	maxAge := int(expires.Sub(s.now()) / time.Second)
	if maxAge <= 0 {
		maxAge = -1 // Max-Age=0: delete it
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", MaxAge: maxAge,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// clearCookie deletes the session cookie (Max-Age=0). It replaces a cookie
// that this response already set, because the server sets no other cookie.
func (s *Server) clearCookie(w http.ResponseWriter) {
	w.Header().Del("Set-Cookie")
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}
