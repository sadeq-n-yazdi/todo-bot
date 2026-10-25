package web

import (
	"strings"
	"testing"
)

// TestCSPText pins the page policies of docs/WEB-DESIGN.md §11.2. A change of
// a policy must change this test too, so that no host is added by accident.
func TestCSPText(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"pwa", cspPWA, "default-src 'none'; script-src 'self' https://telegram.org; " +
			"style-src 'self' https://fonts.googleapis.com; img-src 'self' data:; connect-src 'self'; " +
			"manifest-src 'self'; worker-src 'self'; frame-src https://oauth.telegram.org; " +
			"font-src 'self' https://fonts.gstatic.com; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"},
		{"miniapp", cspMiniApp, "default-src 'none'; script-src 'self' https://telegram.org; " +
			"style-src 'self' https://fonts.googleapis.com; img-src 'self' data:; connect-src 'self'; " +
			"font-src 'self' https://fonts.gstatic.com; base-uri 'none'; form-action 'self'; " +
			"frame-ancestors https://web.telegram.org"},
		{"api", cspAPI, "default-src 'none'; frame-ancestors 'none'"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, c.got, c.want)
		}
	}
}

// TestWidgetHostsOnlyOnThePWA: the widget hosts of §11.2 are on "/" only.
// The Mini App and the API never frame oauth.telegram.org.
func TestWidgetHostsOnlyOnThePWA(t *testing.T) {
	for name, csp := range map[string]string{"miniapp": cspMiniApp, "api": cspAPI} {
		if strings.Contains(csp, "oauth.telegram.org") || strings.Contains(csp, "frame-src") {
			t.Errorf("%s allows the widget frame: %q", name, csp)
		}
	}
	h := newHarness(t)
	w := h.do("GET", "/", "")
	if got := w.Header().Get("Content-Security-Policy"); got != cspPWA {
		t.Errorf("/ has CSP %q", got)
	}
	if got := h.do("GET", "/tg/", "").Header().Get("Content-Security-Policy"); got != cspMiniApp {
		t.Errorf("/tg/ has CSP %q", got)
	}
}
