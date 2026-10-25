package web

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticFiles(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		path, body, cache, csp, ctype string
		coop                          bool
	}{
		{"/", "<!doctype html><title>pwa</title>", "no-cache", cspPWA, "text/html; charset=utf-8", true},
		{"/index.html", "<!doctype html><title>pwa</title>", "no-cache", cspPWA, "text/html; charset=utf-8", true},
		{"/tg/", "<!doctype html><title>miniapp</title>", "no-cache", cspMiniApp, "text/html; charset=utf-8", false},
		{"/assets/app-1a2b3c.js", "console.log(1)", "public, max-age=31536000, immutable", cspPWA, "text/javascript; charset=utf-8", false},
		{"/assets/app-1a2b3c.css", "body{}", "public, max-age=31536000, immutable", cspPWA, "text/css; charset=utf-8", false},
		{"/assets/sub/nested-ab.txt", "n", "public, max-age=31536000, immutable", cspPWA, "text/plain; charset=utf-8", false},
		{"/icons/icon-192.png", "\x89PNG", "public, max-age=86400", cspPWA, "image/png", false},
		{"/manifest.webmanifest", `{"name":"Todo"}`, "no-cache", cspPWA, "application/manifest+json", false},
		{"/sw.js", "self.x=1", "no-cache", cspPWA, "text/javascript; charset=utf-8", false},
	}
	for _, c := range cases {
		w := h.do("GET", c.path, "")
		hd := w.Header()
		if w.Code != 200 || w.Body.String() != c.body {
			t.Errorf("%s: %d %q", c.path, w.Code, w.Body)
			continue
		}
		if hd.Get("Cache-Control") != c.cache {
			t.Errorf("%s: Cache-Control %q, want %q", c.path, hd.Get("Cache-Control"), c.cache)
		}
		if hd.Get("Content-Security-Policy") != c.csp {
			t.Errorf("%s: CSP %q", c.path, hd.Get("Content-Security-Policy"))
		}
		if hd.Get("Content-Type") != c.ctype {
			t.Errorf("%s: Content-Type %q, want %q", c.path, hd.Get("Content-Type"), c.ctype)
		}
		if got := hd.Get("Cross-Origin-Opener-Policy"); (got == "same-origin") != c.coop {
			t.Errorf("%s: COOP %q", c.path, got)
		}
	}
	if w := h.do("HEAD", "/", ""); w.Code != 200 {
		t.Errorf("HEAD /: %d", w.Code)
	}
}

func TestStaticMissing(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/assets/missing.js", "/assets/", "/assets/sub", "/assets/sub/", "/icons/x.png",
		"/icons/", "/nothing", "/tg/other", "/placeholder/index.html", "/tg/index.html/"} {
		w := h.do("GET", p, "")
		if w.Code != 404 && w.Code != 301 {
			t.Errorf("%s: %d", p, w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q on a %d", p, cc, w.Code)
		}
	}
	if w := h.do("POST", "/", "", delHeader("X-Todo-Request")); w.Code != 405 {
		t.Errorf("POST /: %d", w.Code)
	}
}

func TestMiniAppRedirect(t *testing.T) {
	h := newHarness(t)
	w := h.do("GET", "/tg?a=signout", "")
	if w.Code != 301 || w.Header().Get("Location") != "/tg/?a=signout" {
		t.Fatalf("/tg: %d %q", w.Code, w.Header().Get("Location"))
	}
}

// A binary with the placeholder has no tg/index.html: "/tg/" shows the
// placeholder page, with the Mini App CSP.
func TestPlaceholderMiniApp(t *testing.T) {
	h := newHarness(t)
	h.srv.d.Assets = fstest.MapFS{"index.html": {Data: []byte("placeholder")}}
	w := h.do("GET", "/tg/", "")
	if w.Code != 200 || w.Body.String() != "placeholder" || w.Header().Get("Content-Security-Policy") != cspMiniApp {
		t.Fatalf("/tg/: %d %q %q", w.Code, w.Body, w.Header().Get("Content-Security-Policy"))
	}
	h.srv.d.Assets = Assets()
	if w := h.do("GET", "/", ""); w.Code != 200 {
		t.Fatalf("/ with the real Assets(): %d", w.Code)
	}
}

// The PWA page gets the bot name as <meta name="todo-bot">, so that the login
// screen can link to the Mini App. The Mini App page does not get it.
func TestPageBotNameMeta(t *testing.T) {
	h := newHarness(t)
	h.srv.d.Assets = fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><html><head><title>pwa</title></head><body></body></html>")},
		"tg/index.html": {Data: []byte("<!doctype html><html><head><title>tg</title></head><body></body></html>")},
	}
	want := `<!doctype html><html><head><title>pwa</title><meta name="todo-bot" content="TodoBot"></head><body></body></html>`
	for _, p := range []string{"/", "/index.html"} {
		w := h.do("GET", p, "")
		if w.Code != 200 || w.Body.String() != want {
			t.Fatalf("%s: %d %q", p, w.Code, w.Body)
		}
		if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(want)) {
			t.Errorf("%s: Content-Length %q, want %d", p, got, len(want))
		}
		if w.Header().Get("Content-Security-Policy") != cspPWA || w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: headers %v", p, w.Header())
		}
	}
	if w := h.do("HEAD", "/", ""); w.Code != 200 || w.Header().Get("Content-Length") != strconv.Itoa(len(want)) {
		t.Errorf("HEAD /: %d %q", w.Code, w.Header().Get("Content-Length"))
	}
	if w := h.do("GET", "/tg/", ""); w.Code != 200 || strings.Contains(w.Body.String(), "todo-bot") {
		t.Errorf("/tg/: %d %q", w.Code, w.Body)
	}

	// The name is HTML-escaped.
	h.srv.d.BotName = `a"b<c>&d`
	if w := h.do("GET", "/", ""); !strings.Contains(w.Body.String(), `<meta name="todo-bot" content="a&#34;b&lt;c&gt;&amp;d"></head>`) {
		t.Errorf("escaped: %q", w.Body)
	}

	// No name, or a page without </head>: the page is unchanged.
	h.srv.d.BotName = ""
	if w := h.do("GET", "/", ""); strings.Contains(w.Body.String(), "todo-bot") {
		t.Errorf("no name: %q", w.Body)
	}
	h.srv.d.BotName = "TodoBot"
	h.srv.d.Assets = fstest.MapFS{"index.html": {Data: []byte("placeholder")}}
	if w := h.do("GET", "/", ""); w.Body.String() != "placeholder" {
		t.Errorf("no </head>: %q", w.Body)
	}
}
