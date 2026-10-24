package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// Server limits (docs/WEB-DESIGN.md §9.2, §11.3).
const (
	maxBody           = 16 << 10 // bytes of a request body
	maxInFlight       = 64       // requests that run at the same time
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 8 << 10
)

// Server is the web server: the static files, the JSON API and the
// sign-in. It is an http.Handler; Serve and Shutdown run it on a listener.
// All state that matters is in the database, so any node can answer any
// request. The rate limits are in memory, on each node (§10).
type Server struct {
	d      Deps
	log    *slog.Logger
	mux    *http.ServeMux
	root   http.Handler
	hs     *http.Server
	host   string // host (and port) of TODO_WEB_ORIGIN, lower case
	origin string // TODO_WEB_ORIGIN
	sem    chan struct{}
	lim    limits

	// The state of the later sub-issues. Each one belongs to its own file.
	members memberState // membership.go
	codes   codeState   // auth_code.go
}

// New returns the server of d. d.Config must hold a valid WebOrigin
// (config.Load checks it); with an invalid one the Host check refuses
// every request.
func New(d Deps) *Server {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Server{d: d, log: log, mux: http.NewServeMux(), sem: make(chan struct{}, maxInFlight), lim: newLimits(d.Clock)}
	if u, err := url.Parse(d.Config.WebOrigin); err == nil && u.Host != "" {
		s.host = strings.ToLower(u.Host)
		s.origin = strings.ToLower(u.Scheme + "://" + u.Host)
	}
	s.initMembers()
	s.initCodes()
	s.routes()
	s.root = s.logRequests(s.recoverPanics(s.front(s.mux)))
	s.hs = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.root.ServeHTTP(w, r) }

// Serve answers requests on l until Shutdown or Close. It returns nil after
// Shutdown or Close.
func (s *Server) Serve(l net.Listener) error {
	if err := s.hs.Serve(l); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops the listener and waits for the running requests, until
// ctx ends.
func (s *Server) Shutdown(ctx context.Context) error { return s.hs.Shutdown(ctx) }

// Close stops the listener and every connection at once.
func (s *Server) Close() error { return s.hs.Close() }

func (s *Server) now() time.Time { return s.d.Clock.Now() }

// auth is the session that a route needs (the "Auth" column of §5.3).
type auth int

const (
	authNone    auth = iota
	authSession      // S: any session
	authMiniApp      // S and MA: a miniapp session only
)

// route is one row of the route table.
type route struct {
	pattern string
	h       http.HandlerFunc
	auth    auth
	actor   bool     // the path has {chat}: run withActor (§5.5)
	ip      *limiter // a limit per client IP, before the session check
	user    *limiter // the limit per user; nil means the general one
}

// routes registers the full route table of §5.3. A route that a later
// sub-issue builds has a stub handler that answers 501 not_implemented.
func (s *Server) routes() {
	// 1 to 3: health and the static files.
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("GET /{$}", s.handlePage(false))
	s.mux.HandleFunc("GET /index.html", s.handlePage(false))
	s.mux.HandleFunc("GET /tg", s.handleMiniAppRedirect)
	s.mux.HandleFunc("GET /tg/{$}", s.handlePage(true))
	s.mux.HandleFunc("GET /assets/", s.handleStatic(cacheImmutable))
	s.mux.HandleFunc("GET /icons/", s.handleStatic(cacheIcons))
	s.mux.HandleFunc("GET /manifest.webmanifest", s.handleStatic(cacheNoCache))
	s.mux.HandleFunc("GET /sw.js", s.handleStatic(cacheNoCache))

	for _, rt := range []route{
		{pattern: "POST /api/auth/miniapp", h: s.handleMiniAppSignIn, ip: s.lim.miniApp},                             // 4
		{pattern: "POST /api/auth/logout", h: s.handleLogout, auth: authSession},                                     // 5
		{pattern: "POST /api/auth/logout-all", h: s.handleLogoutAll, auth: authSession},                              // 6
		{pattern: "GET /api/me", h: s.handleMe, auth: authSession},                                                   // 7
		{pattern: "GET /api/i18n", h: s.handleI18n},                                                                  // 8
		{pattern: "GET /api/chats", h: s.handleChats, auth: authSession},                                             // 9
		{pattern: "GET /api/chats/{chat}/items", h: s.handleListItems, auth: authSession, actor: true},               // 10
		{pattern: "GET /api/assigned", h: s.handleAssigned, auth: authSession},                                       // 11
		{pattern: "POST /api/auth/code", h: s.handleNewCode, auth: authMiniApp, user: s.lim.code},                    // 12
		{pattern: "POST /api/auth/code/redeem", h: s.handleRedeemCode, ip: s.lim.redeem},                             // 13
		{pattern: "DELETE /api/auth/code", h: s.handleCancelCode, auth: authMiniApp},                                 // 14
		{pattern: "POST /api/parse", h: s.handleParse, auth: authSession},                                            // 15
		{pattern: "GET /api/chats/{chat}/members", h: s.handleMembers, auth: authSession, actor: true},               // 16
		{pattern: "POST /api/chats/{chat}/items", h: s.handleAddItem, auth: authSession, actor: true},                // 17
		{pattern: "POST /api/chats/{chat}/items/{id}/done", h: s.handleDoneItem, auth: authSession, actor: true},     // 18
		{pattern: "DELETE /api/chats/{chat}/items/{id}", h: s.handleDeleteItem, auth: authSession, actor: true},      // 19
		{pattern: "POST /api/chats/{chat}/items/{id}/remind", h: s.handleRemindItem, auth: authSession, actor: true}, // 20
		{pattern: "PUT /api/me/settings", h: s.handleSettings, auth: authSession},                                    // 21
		{pattern: "GET /api/chats/{chat}/items/{id}", h: s.handleGetItem, auth: authSession, actor: true},            // 22
		{pattern: "PATCH /api/chats/{chat}/items/{id}", h: s.handlePatchItem, auth: authSession, actor: true},        // 23
		{pattern: "GET /auth/telegram", h: s.handleWidget, ip: s.lim.widget},                                         // 24
	} {
		s.handle(rt)
	}
	// Any other /api/ path gives a JSON 404, not the text 404 of the mux.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, errNotFound) })
}

// handle registers rt with its chain: the IP limit, the session check, the
// user limit, the Mini App check, the actor middleware, then the handler.
func (s *Server) handle(rt route) {
	h := http.Handler(rt.h)
	if rt.actor {
		h = s.withActor(h)
	}
	if rt.auth == authMiniApp {
		h = s.miniAppOnly(h)
	}
	if rt.auth != authNone {
		lim := rt.user
		if lim == nil {
			lim = s.lim.user
		}
		h = s.withSession(s.userLimit(lim, h))
	}
	if rt.ip != nil {
		h = s.ipLimit(rt.ip, h)
	}
	s.mux.Handle(rt.pattern, h)
}

// front applies the rules that come before any route: the headers of §11.1,
// the Host check, the limit of requests at the same time, and, for /api/,
// the CSRF guard of §3.6 and the body limit. /healthz and /readyz skip the
// Host check, and /healthz also skips the request limit.
func (s *Server) front(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCommonHeaders(w.Header())
		health := r.URL.Path == "/healthz" || r.URL.Path == "/readyz"
		if !health && (s.host == "" || !strings.EqualFold(r.Host, s.host)) {
			s.fail(w, r, errMisdirected)
			return
		}
		if r.URL.Path != "/healthz" {
			select {
			case s.sem <- struct{}{}:
				defer func() { <-s.sem }()
			default:
				s.fail(w, r, errBusy)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if unsafeMethod(r.Method) && !s.csrfOK(r) {
				s.fail(w, r, errCSRF)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions && m != http.MethodTrace
}

// csrfOK is the CSRF guard of §3.6. It applies with a cookie, with a bearer
// token and with no session.
func (s *Server) csrfOK(r *http.Request) bool {
	if r.Header.Get("X-Todo-Request") != "1" {
		return false
	}
	site, origin := r.Header.Get("Sec-Fetch-Site"), r.Header.Get("Origin")
	if site == "" && origin == "" {
		return false
	}
	if site != "" && site != "same-origin" {
		return false
	}
	if origin != "" && (s.origin == "" || !strings.EqualFold(origin, s.origin)) {
		return false
	}
	if r.ContentLength != 0 { // a body, or a body of unknown length
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			return false
		}
	}
	return true
}

// decodeJSON reads the JSON body of r into v. It refuses unknown fields and
// data after the value (400 bad_request), and a body above maxBody
// (413 too_large).
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if _, terr := dec.Token(); !errors.Is(terr, io.EOF) {
			err = errors.New("data after the JSON value")
		}
	}
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errTooLarge
		}
		return errBadRequest
	}
	return nil
}

// ipLimit applies a limit per client IP.
func (s *Server) ipLimit(l *limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := l.allow(s.clientIP(r).String()); !ok {
			s.fail(w, r, errRateLimited.retry(retry))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// reqInfo is what the middleware learns about a request. The log line
// reads it after the handler returns.
type reqInfo struct {
	loc  i18n.Locale
	sess *session // nil without a session
}

type reqInfoKey struct{}

func infoOf(r *http.Request) *reqInfo {
	ri, _ := r.Context().Value(reqInfoKey{}).(*reqInfo)
	return ri
}

// localeOf is the locale for the text of a response: the user's locale with
// a session, else the first language of Accept-Language.
func localeOf(r *http.Request) i18n.Locale {
	if ri := infoOf(r); ri != nil {
		return ri.loc
	}
	return acceptLocale(r)
}

func acceptLocale(r *http.Request) i18n.Locale {
	first, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ",")
	first, _, _ = strings.Cut(first, ";")
	return i18n.Default(strings.TrimSpace(first))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// logRequests logs one line for each request. The line holds only the
// method, the route pattern, the status, the duration and the user ID: never
// the URL (the widget query holds a proof), a header, a cookie or a body.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ri := &reqInfo{loc: acceptLocale(r)}
		r = r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, ri))
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		attrs := []any{"method", r.Method, "route", r.Pattern, "status", sw.status, "dur", time.Since(start)}
		if ri.sess != nil {
			attrs = append(attrs, "user", ri.sess.UserID)
		}
		s.log.Log(r.Context(), level, "web request", attrs...)
	})
}

// recoverPanics turns a panic in a handler into 500 internal. It logs only
// the route, never the panic value, which can hold request data.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(v)
			}
			s.log.Error("web: handler panic", "route", r.Pattern)
			s.fail(w, r, errInternal)
		}()
		next.ServeHTTP(w, r)
	})
}

// handleHealthz is endpoint 1: liveness. It does not touch the database.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok")
}

// handleReadyz is endpoint 2: one small database read.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if ok, retry := s.lim.readyz.allow(s.clientIP(r).String()); !ok {
		w.Header().Set("Retry-After", retryAfter(retry))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "rate limited")
		return
	}
	if _, err := s.d.Web.GroupChats(r.Context(), 1); err != nil {
		s.log.Warn("web: readyz", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "unavailable")
		return
	}
	_, _ = io.WriteString(w, "ok")
}
