package web

import (
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
