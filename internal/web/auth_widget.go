package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// Limits of the Telegram Login Widget (docs/WEB-DESIGN.md §3.4).
const (
	// widgetProofAge is the age limit of a widget proof. A proof is also
	// stored as used until auth_date + widgetProofAge, so it works one time.
	widgetProofAge = 10 * time.Minute
	// maxWidgetQuery is the largest accepted query of the redirect.
	maxWidgetQuery = 4 << 10

	// widgetStartQuery is the query of the start request: the login screen
	// sends GET /auth/telegram?start=1 before it shows the widget.
	widgetStartQuery = "start=1"
	// widgetCookie holds the nonce of the start request. Only a browser that
	// showed the widget on the login screen has it, so a link with the proof
	// of another person does not sign anyone in (login CSRF). It is Lax,
	// because the redirect from oauth.telegram.org is a cross-site
	// navigation.
	widgetCookie = "__Host-tb_widget"
	// widgetNonceAge is the life of the nonce cookie.
	widgetNonceAge = 10 * time.Minute
	widgetNonceLen = 43 // 32 random bytes as base64url, no padding
)

// The error codes of the widget. They go to the login screen in the URL
// fragment (/#login-error=<code>), and the screen shows its web.widget.*
// text for each one.
const (
	widgetBadRequest   = "bad_request"
	widgetBadSignature = "bad_signature"
	widgetExpired      = "expired"
	widgetNotAllowed   = "not_allowed"
	widgetUsed         = "used"
	widgetInternal     = "internal"
	// widgetRestart: no nonce cookie (the widget was not started on this
	// browser, or more than 10 minutes ago).
	widgetRestart = "restart"
	// widgetOtherUser: the browser is signed in as another user.
	widgetOtherUser = "other_user"
)

// widgetFieldNames are the fields that Telegram documents for the redirect.
// Any other key is refused, so that the check string and the user come from
// the same, known values.
var widgetFieldNames = map[string]bool{
	"id": true, "first_name": true, "last_name": true, "username": true,
	"photo_url": true, "auth_date": true, "hash": true,
}

// widgetUser is the verified content of a widget redirect.
type widgetUser struct {
	ID       int64
	Username string
	AuthDate time.Time
	hash     []byte // the decoded HMAC: the key of the proof
}

// verifyWidget does steps 3 to 5 of §3.4 (not the allowlist), with the rule
// of https://core.telegram.org/widgets/login#checking-authorization: the
// data-check string is every field except hash, sorted by key, as
// "key=value" lines; the secret key is SHA256(botToken); hash is the hex
// HMAC-SHA256 of the data-check string. It accepts only the documented
// fields, each one time, with no line feed. It returns one of the widget
// error codes, or "".
func verifyWidget(rawQuery, botToken string, now time.Time) (widgetUser, string) {
	if rawQuery == "" || len(rawQuery) > maxWidgetQuery {
		return widgetUser{}, widgetBadRequest
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return widgetUser{}, widgetBadRequest
	}
	fields := make(map[string]string, len(values))
	for k, v := range values {
		if !widgetFieldNames[k] || len(v) != 1 || strings.ContainsAny(v[0], "\r\n") {
			return widgetUser{}, widgetBadRequest
		}
		fields[k] = v[0]
	}
	hashHex := fields["hash"]
	delete(fields, "hash")
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + fields[k]
	}

	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(strings.Join(lines, "\n")))
	got, err := hex.DecodeString(hashHex)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return widgetUser{}, widgetBadSignature
	}

	// The user comes from the same map as the check string.
	id, err := strconv.ParseInt(fields["id"], 10, 64)
	if err != nil || id <= 0 {
		return widgetUser{}, widgetBadRequest
	}
	sec, err := strconv.ParseInt(fields["auth_date"], 10, 64)
	if err != nil || sec < 0 {
		return widgetUser{}, widgetBadRequest
	}
	authDate := time.Unix(sec, 0)
	// Strict: at auth_date + widgetProofAge the store forgets the used
	// proof, so the proof must stop working at that moment too.
	if now.Sub(authDate) >= widgetProofAge || authDate.Sub(now) > maxInitDataFuture {
		return widgetUser{}, widgetExpired
	}
	return widgetUser{ID: id, Username: fields["username"], AuthDate: authDate.UTC(), hash: got}, ""
}

// proofKey is the key of a used proof: the hex SHA-256 of the decoded hash.
// The decoded bytes make "ABCD" and "abcd" one proof.
func proofKey(hash []byte) string {
	sum := sha256.Sum256(hash)
	return hex.EncodeToString(sum[:])
}

// handleWidget is endpoint 24, GET /auth/telegram. The IP limit of the route
// comes first.
//
// With the query start=1 it is the start request of the login screen
// (handleWidgetStart). Any other query is the redirect of the Telegram Login
// Widget (§3.4). The redirect always answers 303: to "/" with the session
// cookie, or to /#login-error=<code>. Neither the URL nor the log gets a
// received field.
func (s *Server) handleWidget(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery == widgetStartQuery {
		s.handleWidgetStart(w, r)
		return
	}
	// The nonce works one time: clear it on every answer.
	nonce, err := r.Cookie(widgetCookie)
	s.setWidgetCookie(w, "", -1)
	if err != nil || !validNonce(nonce.Value) {
		widgetRedirect(w, widgetRestart)
		return
	}
	now := s.now()
	u, code := verifyWidget(r.URL.RawQuery, s.d.Token, now)
	if code != "" {
		widgetRedirect(w, code)
		return
	}
	// 5. The allowlist (§3.2 step 10), before the proof is used.
	if !s.d.Allowed[u.ID] {
		widgetRedirect(w, widgetNotAllowed)
		return
	}
	// A browser that is signed in as another user keeps its session.
	if other, err := s.sessionUser(r); err != nil {
		s.widgetFail(w, r, err)
		return
	} else if other != 0 && other != u.ID {
		widgetRedirect(w, widgetOtherUser)
		return
	}
	// 6. Each proof works one time, in the store, on any node.
	fresh, err := s.d.Web.UseProof(r.Context(), proofKey(u.hash), u.AuthDate.Add(widgetProofAge))
	if err != nil {
		s.widgetFail(w, r, err)
		return
	}
	if !fresh {
		widgetRedirect(w, widgetUsed)
		return
	}
	// 7. The user (created on first contact, as the bot does), the session,
	// the cookie and the notice.
	prefs, err := s.d.Svc.Prefs(r.Context(), u.ID, "")
	if err != nil {
		s.widgetFail(w, r, err)
		return
	}
	if err := s.d.Svc.NoteUsername(r.Context(), u.ID, u.Username); err != nil {
		s.log.Warn("web: note username", "user", u.ID, "err", err)
	}
	token, ws, err := s.d.Web.StartWebSession(r.Context(), u.ID, model.WebBrowser, r.UserAgent(),
		s.d.Config.WebSessionTTL, s.d.Config.WebSessionMax)
	if err != nil {
		s.widgetFail(w, r, err)
		return
	}
	if ri := infoOf(r); ri != nil {
		ri.loc = prefs.Locale
		ri.sess = &session{WebSession: ws, prefs: prefs} // for the log line: the user ID only
	}
	s.setCookie(w, token, ws.ExpiresAt)
	w.Header().Set("Location", "/")
	w.WriteHeader(http.StatusSeeOther)
	s.sendSignInNotice(r, u.ID, prefs.Locale)
}

// handleWidgetStart is GET /auth/telegram?start=1. The login screen calls it
// with fetch before it shows the widget. It sets the nonce cookie and
// answers 204. A cross-site page cannot send it: it needs the header
// X-Todo-Request (no CORS preflight is ever answered) and Sec-Fetch-Site:
// same-origin. A browser that is signed in gets no nonce (403
// wrong_session), because the login screen shows only without a session.
func (s *Server) handleWidgetStart(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Todo-Request") != "1" || r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		s.fail(w, r, errCSRF)
		return
	}
	if uid, err := s.sessionUser(r); err != nil {
		s.fail(w, r, err)
		return
	} else if uid != 0 {
		s.fail(w, r, errWrongSession)
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		s.fail(w, r, err)
		return
	}
	s.setWidgetCookie(w, base64.RawURLEncoding.EncodeToString(b), int(widgetNonceAge/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

// sessionUser is the user of a live browser session cookie of r, or 0. The
// session cookie is SameSite=Strict, so a cross-site redirect does not send
// it; then this gives 0.
func (s *Server) sessionUser(r *http.Request) (int64, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return 0, nil
	}
	ws, err := s.d.Web.WebSession(r.Context(), c.Value, s.d.Config.WebSessionTTL)
	if errors.Is(err, service.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !s.d.Allowed[ws.UserID] {
		return 0, nil
	}
	return ws.UserID, nil
}

// validNonce reports whether v has the form of a nonce. The browser keeps
// the cookie for 10 minutes only, and no other site can set a __Host-
// cookie for this host, so the form is enough.
func validNonce(v string) bool {
	if len(v) != widgetNonceLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil
}

// setWidgetCookie sets the nonce cookie, or deletes it with maxAge < 0.
func (s *Server) setWidgetCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: widgetCookie, Value: value, Path: "/", MaxAge: maxAge,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// widgetFail logs an internal error (never a received field) and sends the
// browser to the login screen.
func (s *Server) widgetFail(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("web: widget sign-in", "route", r.Pattern, "err", err)
	widgetRedirect(w, widgetInternal)
}

// widgetRedirect answers 303 to the login screen with the error code in the
// fragment. The code is one of the fixed widget codes.
func widgetRedirect(w http.ResponseWriter, code string) {
	w.Header().Set("Location", "/#login-error="+code)
	w.WriteHeader(http.StatusSeeOther)
}
