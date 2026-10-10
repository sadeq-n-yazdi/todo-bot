// Package migrations embeds the SQL schema migrations. Files are named
// NNNN_description.sql and applied in order; PRAGMA user_version records
// the last applied number.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
