package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// apiError is an error that the API sends to the client as
// {"error": {"code", "message", ...Extra}} (docs/WEB-DESIGN.md §5.1). The
// message is the web.err.<code> string in the language of the request.
type apiError struct {
	Status int
	Code   string
	// Extra holds more fields of the "error" object, for example user_id
	// for not_allowed or new_id for group_moved. Its values must not hold
	// a secret.
	Extra map[string]any
	// RetryAfter, if positive, is sent as the Retry-After header.
	RetryAfter time.Duration
}

func (e *apiError) Error() string { return "web: " + strconv.Itoa(e.Status) + " " + e.Code }

// with returns a copy of e with one more field in the error object.
func (e *apiError) with(key string, v any) *apiError {
	c := *e
	c.Extra = make(map[string]any, len(e.Extra)+1)
	for k, x := range e.Extra {
		c.Extra[k] = x
	}
	c.Extra[key] = v
	return &c
}

// retry returns a copy of e with a Retry-After of d, rounded up to a second.
func (e *apiError) retry(d time.Duration) *apiError {
	c := *e
	c.RetryAfter = d
	return &c
}

// Every error code of the API (docs/WEB-DESIGN.md §5). Each code has a
// web.err.<code> string in English and Farsi (messages_web.go).
var (
	errBadRequest     = &apiError{Status: http.StatusBadRequest, Code: "bad_request"}
	errBadSignature   = &apiError{Status: http.StatusUnauthorized, Code: "bad_signature"}
	errExpired        = &apiError{Status: http.StatusUnauthorized, Code: "expired"}
	errNotAllowed     = &apiError{Status: http.StatusForbidden, Code: "not_allowed"}
	errUnauthn        = &apiError{Status: http.StatusUnauthorized, Code: "unauthenticated"}
	errRevoked        = &apiError{Status: http.StatusUnauthorized, Code: "session_revoked"}
	errCSRF           = &apiError{Status: http.StatusForbidden, Code: "csrf"}
	errTooLarge       = &apiError{Status: http.StatusRequestEntityTooLarge, Code: "too_large"}
	errRateLimited    = &apiError{Status: http.StatusTooManyRequests, Code: "rate_limited"}
	errInternal       = &apiError{Status: http.StatusInternalServerError, Code: "internal"}
	errNotImplemented = &apiError{Status: http.StatusNotImplemented, Code: "not_implemented"}
	errNotFound       = &apiError{Status: http.StatusNotFound, Code: "not_found"}
	errNotMember      = &apiError{Status: http.StatusForbidden, Code: "not_member"}
	errGroupMoved     = &apiError{Status: http.StatusConflict, Code: "group_moved"}
	errUnverified     = &apiError{Status: http.StatusServiceUnavailable, Code: "membership_unverified"}
	errBadLang        = &apiError{Status: http.StatusBadRequest, Code: "bad_lang"}
	errBadLangInput   = &apiError{Status: http.StatusUnprocessableEntity, Code: "bad_lang"}
	errBadCalendar    = &apiError{Status: http.StatusUnprocessableEntity, Code: "bad_calendar"}
	errWrongUser      = &apiError{Status: http.StatusForbidden, Code: "wrong_user"}
	errTooManyCodes   = &apiError{Status: http.StatusServiceUnavailable, Code: "too_many_codes"}
	errBadCode        = &apiError{Status: http.StatusUnauthorized, Code: "bad_code"}
	errEmptyText      = &apiError{Status: http.StatusUnprocessableEntity, Code: "empty_text"}
	errTooLong        = &apiError{Status: http.StatusUnprocessableEntity, Code: "too_long"}
	errUnknownAssign  = &apiError{Status: http.StatusUnprocessableEntity, Code: "unknown_assignee"}
	errInPast         = &apiError{Status: http.StatusUnprocessableEntity, Code: "in_past"}
	errBadTime        = &apiError{Status: http.StatusUnprocessableEntity, Code: "bad_time"}
	// errWrongSession: a browser session called an endpoint that accepts
	// only a Mini App session (issue #115).
	errWrongSession = &apiError{Status: http.StatusForbidden, Code: "wrong_session"}
	// errMisdirected: the Host header is not the host of TODO_WEB_ORIGIN
	// (§9.3, §11.3).
	errMisdirected = &apiError{Status: http.StatusMisdirectedRequest, Code: "misdirected"}
	// errBusy: 64 requests already run on this node (§9.2).
	errBusy = &apiError{Status: http.StatusServiceUnavailable, Code: "busy"}
)

// allErrors lists every error above, for the catalog test.
var allErrors = []*apiError{
	errBadRequest, errBadSignature, errExpired, errNotAllowed, errUnauthn, errRevoked,
	errCSRF, errTooLarge, errRateLimited, errInternal, errNotImplemented, errNotFound,
	errNotMember, errGroupMoved, errUnverified, errBadLang, errBadLangInput, errBadCalendar,
	errWrongUser, errTooManyCodes, errBadCode, errEmptyText, errTooLong, errUnknownAssign,
	errInPast, errBadTime, errWrongSession, errMisdirected, errBusy,
}

// writeJSON writes v as the JSON body with the status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail writes err to the client. An *apiError goes out as it is. Any other
// error is logged and gives 500 internal; the client never sees its text.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		s.log.Error("web: internal error", "route", r.Pattern, "err", err)
		ae = errInternal
	}
	body := make(map[string]any, len(ae.Extra)+2)
	for k, v := range ae.Extra {
		body[k] = v
	}
	body["code"] = ae.Code
	body["message"] = localeOf(r).T("web.err." + ae.Code)
	if ae.RetryAfter > 0 {
		w.Header().Set("Retry-After", retryAfter(ae.RetryAfter))
	}
	writeJSON(w, ae.Status, map[string]any{"error": body})
}

// retryAfter is d in whole seconds, rounded up, at least 1.
func retryAfter(d time.Duration) string {
	return strconv.Itoa(max(1, int((d+time.Second-1)/time.Second)))
}
