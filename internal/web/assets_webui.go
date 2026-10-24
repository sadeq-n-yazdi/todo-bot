//go:build webui

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the built front end: the files that Vite wrote to dist/.
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // "dist" is a fixed, valid path
	}
	return sub
}

// Embedded reports whether the binary holds the real front end.
func Embedded() bool { return true }
