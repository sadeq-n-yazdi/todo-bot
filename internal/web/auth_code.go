package web

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

// Limits of the login code (docs/WEB-DESIGN.md §3.3). The code itself (8
// Crockford base32 characters, 5 minutes, at most 100 live, one for each
// user) is made and stored by service.Web.
const (
	// codeProofAge is the age limit of the initData that makes a code.
	codeProofAge = 10 * time.Minute

	// Failed redeems: at most ipFailMax in ipFailWindow for one IP, and at
	// most nodeFailMax in nodeFailWindow for all IPs of this node.
	ipFailMax      = 5
	ipFailWindow   = 10 * time.Minute
	nodeFailMax    = 60
	nodeFailWindow = time.Hour

	// maxFailKeys is the number of IPs that the failure counter keeps.
	maxFailKeys = 10000
)

// codeState is the in-memory state of the login codes on this node: the
// redeem failure limits (§3.3, §10). They only slow down guessing on this
// node. The correctness of a code (one use, expiry, the cap) is in the
// store, and no node needs the state of another node.
type codeState struct {
	mu   sync.Mutex
	byIP map[string][]time.Time
	node []time.Time

	// notices counts the sign-in notices that are in flight (tests wait on it).
	notices sync.WaitGroup
}

// initCodes sets up s.codes. New calls it.
func (s *Server) initCodes() { s.codes.byIP = map[string][]time.Time{} }

// prune removes the times that are older than the window.
func prune(ts []time.Time, now time.Time, window time.Duration) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].Add(window).After(now) {
		i++
	}
	return ts[i:]
}

// reserve counts one failed redeem for ip before the attempt, so that
// parallel guesses cannot pass the limit. It returns false, with the time
// to wait, if a limit is reached. A redeem that works calls release.
func (c *codeState) reserve(ip string, now time.Time) (ok bool, retry time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.node = prune(c.node, now, nodeFailWindow)
	if len(c.node) >= nodeFailMax {
		return false, c.node[0].Add(nodeFailWindow).Sub(now)
	}
	ts := prune(c.byIP[ip], now, ipFailWindow)
	if len(ts) >= ipFailMax {
		c.byIP[ip] = ts
		return false, ts[0].Add(ipFailWindow).Sub(now)
	}
	if _, known := c.byIP[ip]; !known && len(c.byIP) >= maxFailKeys {
		c.dropOld(now)
	}
	c.byIP[ip] = append(ts, now)
	c.node = append(c.node, now)
	return true, 0
}

// release takes back the count of reserve (the redeem worked).
func (c *codeState) release(ip string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byIP[ip] = removeOne(c.byIP[ip], now)
	if len(c.byIP[ip]) == 0 {
		delete(c.byIP, ip)
	}
	c.node = removeOne(c.node, now)
}

func removeOne(ts []time.Time, t time.Time) []time.Time {
	for i := len(ts) - 1; i >= 0; i-- {
		if ts[i].Equal(t) {
			return append(ts[:i:i], ts[i+1:]...)
		}
	}
	return ts
}

// dropOld removes the IPs with no recent failure; if all are recent, it
// removes one. The caller holds the lock.
func (c *codeState) dropOld(now time.Time) {
	for k, ts := range c.byIP {
		if len(prune(ts, now, ipFailWindow)) == 0 {
			delete(c.byIP, k)
		}
	}
	for k := range c.byIP {
		if len(c.byIP) < maxFailKeys {
			break
		}
		delete(c.byIP, k)
	}
}

// failKey is the key of the failure counter of a client: the IP, and for
// IPv6 the /64 prefix, because one client often owns a whole /64.
func failKey(a netip.Addr) string {
	if a.Is6() && !a.Is4In6() {
		return netip.PrefixFrom(a, 64).Masked().Addr().String()
	}
	return a.String()
}

type codeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleNewCode is endpoint 12, POST /api/auth/code (Mini App only). The
// session is not enough: the body must hold initData that is at most 10
// minutes old and that belongs to the user of the session. A request with
// no initData answers 401 bad_signature.
func (s *Server) handleNewCode(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req struct {
		InitData string `json:"init_data"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if req.InitData == "" { // the token alone proves nothing
		s.fail(w, r, errBadSignature)
		return
	}
	d, err := verifyInitData(req.InitData, s.d.Token, s.now(), codeProofAge)
	if err != nil {
		s.fail(w, r, initDataError(err))
		return
	}
	if d.UserID != sess.UserID {
		s.fail(w, r, errWrongUser)
		return
	}
	if !s.d.Allowed[d.UserID] {
		s.fail(w, r, errNotAllowed.with("user_id", d.UserID))
		return
	}
	code, c, err := s.d.Web.NewLoginCode(r.Context(), d.UserID)
	if err != nil {
		if errors.Is(err, service.ErrTooManyCodes) {
			err = errTooManyCodes
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, codeResponse{Code: showCode(code), ExpiresAt: c.ExpiresAt.UTC()})
}

// showCode writes an 8-character code as ABCD-EFGH.
func showCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

type redeemResponse struct {
	User      miniAppUser `json:"user"`
	ExpiresAt time.Time   `json:"expires_at"`
}

// handleRedeemCode is endpoint 13, POST /api/auth/code/redeem. The code
// comes only in the JSON body, and it goes to no log.
func (s *Server) handleRedeemCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		s.fail(w, r, errBadRequest)
		return
	}
	ip, now := failKey(s.clientIP(r)), s.now()
	if ok, retry := s.codes.reserve(ip, now); !ok {
		s.fail(w, r, errRateLimited.retry(retry))
		return
	}
	// One conditional delete in the store: of any number of parallel
	// redeems of one code, on any node, one gets the row.
	c, err := s.d.Web.RedeemLoginCode(r.Context(), req.Code)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			err = errBadCode // the failure stays counted
		} else {
			s.codes.release(ip, now) // a store error is not a wrong guess
		}
		s.fail(w, r, err)
		return
	}
	s.codes.release(ip, now)
	if !s.d.Allowed[c.UserID] {
		s.fail(w, r, errNotAllowed.with("user_id", c.UserID))
		return
	}
	prefs, err := s.d.Svc.Prefs(r.Context(), c.UserID, "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ri := infoOf(r)
	ri.loc = prefs.Locale
	token, ws, err := s.d.Web.StartWebSession(r.Context(), c.UserID, model.WebBrowser, r.UserAgent(),
		s.d.Config.WebSessionTTL, s.d.Config.WebSessionMax)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ri.sess = &session{WebSession: ws, prefs: prefs} // for the log line: the user ID only
	s.setCookie(w, token, ws.ExpiresAt)
	writeJSON(w, http.StatusOK, redeemResponse{User: miniAppUser{ID: c.UserID}, ExpiresAt: ws.ExpiresAt.UTC()})
	// The notice goes out in the background. A send error gives only a
	// warning; the session stays valid.
	s.sendSignInNotice(r, c.UserID, prefs.Locale)
}

// handleCancelCode is endpoint 14, DELETE /api/auth/code (Mini App only).
func (s *Server) handleCancelCode(w http.ResponseWriter, r *http.Request) {
	if err := s.d.Web.CancelLoginCodes(r.Context(), sessionFrom(r).UserID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
