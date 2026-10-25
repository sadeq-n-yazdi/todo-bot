package web

import (
	"flag"

	// The golden files use Asia/Tehran; embed the zone data so the output
	// does not depend on the host.
	_ "time/tzdata"
)

// updateGolden rewrites the golden files in testdata/api:
//
//	go test ./internal/web -run Golden -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite the golden files in testdata/api")
