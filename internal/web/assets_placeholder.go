//go:build !webui

package web

import (
	"embed"
	"io/fs"
)

//go:embed placeholder/index.html
var placeholderFS embed.FS

// Assets returns the placeholder page. The binary was built without the
// webui tag, so it has no real front end.
func Assets() fs.FS {
	sub, err := fs.Sub(placeholderFS, "placeholder")
	if err != nil {
		panic(err) // "placeholder" is a fixed, valid path
	}
	return sub
}

// Embedded reports whether the binary holds the real front end.
func Embedded() bool { return false }
