package web

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// The static files come straight from Deps.Assets, the embedded fs.FS
// (docs/WEB-DESIGN.md §8.5). The server does not copy them to memory and
// does not compress them; Caddy and Cloudflare compress.

// handlePage serves the HTML page of the PWA ("/", "/index.html") or of the
// Mini App ("/tg/"), with its CSP. A binary with the placeholder has no
// tg/index.html; then "/tg/" also gets the placeholder page.
func (s *Server) handlePage(miniApp bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := "index.html"
		if miniApp {
			if _, err := fs.Stat(s.d.Assets, "tg/index.html"); err == nil {
				name = "tg/index.html"
			}
		}
		if !s.serveFile(w, r, name, func(h http.Header) { setPageHeaders(h, miniApp) }) {
			http.NotFound(w, r)
		}
	}
}

// handleMiniAppRedirect sends "/tg" to "/tg/", with the same query.
func (s *Server) handleMiniAppRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/tg/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// handleStatic serves one file under its URL path, with cacheControl. A
// missing file or a directory gives 404 with no-store, so that no cache
// keeps the 404.
func (s *Server) handleStatic(cacheControl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if !s.serveFile(w, r, name, func(h http.Header) {
			h.Set("Cache-Control", cacheControl)
			h.Set("Content-Security-Policy", cspPWA)
		}) {
			http.NotFound(w, r)
		}
	}
}

// serveFile serves the regular file name of the assets. It returns false,
// with nothing written, if there is no such file.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string, headers func(http.Header)) bool {
	if s.d.Assets == nil || !fs.ValidPath(name) {
		return false
	}
	f, err := s.d.Assets.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		b, err := io.ReadAll(f)
		if err != nil {
			return false
		}
		rs = bytes.NewReader(b)
	}
	headers(w.Header())
	w.Header().Set("Content-Type", contentType(name))
	http.ServeContent(w, r, name, fi.ModTime().Truncate(time.Second), rs)
	return true
}

func contentType(name string) string {
	switch ext := path.Ext(name); ext {
	case ".webmanifest":
		return "application/manifest+json"
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	default:
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}
