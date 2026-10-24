package web

import (
	"io/fs"
	"strings"
	"testing"
)

// Without the webui tag the package serves the committed placeholder page.
func TestAssetsPlaceholder(t *testing.T) {
	if Embedded() {
		t.Skip("built with the webui tag")
	}
	b, err := fs.ReadFile(Assets(), "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(b), "not built into this binary") {
		t.Errorf("placeholder page has the wrong text: %q", b)
	}
}
