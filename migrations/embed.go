// Package migrations embeds the SQL schema migrations, one directory per
// SQL dialect (sqlite/, and postgres/ and mysql/ from v0.2). Files are named
// NNNN_description.sql and applied in order; every dialect must ship the same
// set of version numbers (see the parity test).
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed sqlite/*.sql
var FS embed.FS

// Dialects lists the dialect directories embedded in FS.
var Dialects = []string{"sqlite"}

// For returns the migrations of one dialect, rooted at its directory.
func For(dialect string) (fs.FS, error) {
	for _, d := range Dialects {
		if d == dialect {
			return fs.Sub(FS, d)
		}
	}
	return nil, fmt.Errorf("migrations: no dialect %q", dialect)
}
