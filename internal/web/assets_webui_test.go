//go:build webui

package web

import (
	"io/fs"
	"strings"
	"testing"
)

// With the webui tag the package serves the Vite output of both entries.
func TestAssetsEmbedded(t *testing.T) {
	if !Embedded() {
		t.Fatal("Embedded() is false with the webui tag")
	}
	for _, name := range []string{"index.html", "tg/index.html"} {
		if _, err := fs.Stat(Assets(), name); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

// The built PWA page has a </head>, so "/" gets the bot name meta. The
// service worker caches this response, so the offline copy has it too.
func TestAssetsEmbeddedBotMeta(t *testing.T) {
	h := newHarness(t)
	h.srv.d.Assets = Assets()
	w := h.do("GET", "/", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<meta name="todo-bot" content="TodoBot"></head>`) {
		t.Fatalf("/: %d %q", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png"`) {
		t.Errorf("/ has no apple-touch-icon: %q", w.Body)
	}
}
