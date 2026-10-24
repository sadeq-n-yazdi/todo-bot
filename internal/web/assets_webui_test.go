//go:build webui

package web

import (
	"io/fs"
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
