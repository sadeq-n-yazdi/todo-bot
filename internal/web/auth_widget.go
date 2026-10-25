package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Limits of the Telegram Login Widget (docs/WEB-DESIGN.md §3.4).
const (
	// widgetProofAge is the age limit of a widget proof. A proof is also
	// stored as used until auth_date + widgetProofAge, so it works one time.
	widgetProofAge = 10 * time.Minute
	// maxWidgetQuery is the largest accepted query of the redirect.
	maxWidgetQuery = 4 << 10
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
)

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
// HMAC-SHA256 of the data-check string. It returns one of the widget error
// codes, or "".
func verifyWidget(rawQuery, botToken string, now time.Time) (widgetUser, string) {
	if rawQuery == "" || len(rawQuery) > maxWidgetQuery {
		return widgetUser{}, widgetBadRequest
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return widgetUser{}, widgetBadRequest
	}
	for _, v := range values {
		if len(v) != 1 {
			return widgetUser{}, widgetBadRequest
		}
	}
	hashHex := values.Get("hash")
	values.Del("hash")
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}

	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(strings.Join(lines, "\n")))
	got, err := hex.DecodeString(hashHex)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return widgetUser{}, widgetBadSignature
	}

	id, err := strconv.ParseInt(values.Get("id"), 10, 64)
	if err != nil || id <= 0 {
		return widgetUser{}, widgetBadRequest
	}
	sec, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || sec < 0 {
		return widgetUser{}, widgetBadRequest
	}
	authDate := time.Unix(sec, 0)
	// Strict: at auth_date + widgetProofAge the store forgets the used
	// proof, so the proof must stop working at that moment too.
	if now.Sub(authDate) >= widgetProofAge || authDate.Sub(now) > maxInitDataFuture {
		return widgetUser{}, widgetExpired
	}
	return widgetUser{ID: id, Username: values.Get("username"), AuthDate: authDate.UTC(), hash: got}, ""
}

// proofKey is the key of a used proof: the hex SHA-256 of the decoded hash.
// The decoded bytes make "ABCD" and "abcd" one proof.
func proofKey(hash []byte) string {
	sum := sha256.Sum256(hash)
	return hex.EncodeToString(sum[:])
}

// handleWidget is endpoint 24, GET /auth/telegram: the redirect of the
// Telegram Login Widget (§3.4). It always answers 303: to "/" with the
// session cookie, or to /#login-error=<code>. Neither the URL nor the log
// gets a received field. The IP limit of the route comes first.
func (s *Server) handleWidget(w http.ResponseWriter, r *http.Request) {
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
