package migrations

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var nameRE = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

// Every dialect must ship the same migration versions, so a database can
// move between providers at any version.
func TestVersionParity(t *testing.T) {
	var want []string
	for i, d := range Dialects {
		sub, err := For(d)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := fs.ReadDir(sub, ".")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries {
			m := nameRE.FindStringSubmatch(e.Name())
			if m == nil {
				t.Errorf("%s/%s: bad migration file name", d, e.Name())
				continue
			}
			got = append(got, m[1])
		}
		if len(got) == 0 {
			t.Errorf("%s: no migrations", d)
		}
		if i == 0 {
			want = got
		} else if !slices.Equal(got, want) {
			t.Errorf("%s versions %v, %s has %v", d, got, Dialects[0], want)
		}
	}
	if _, err := For("nosuch"); err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("For(nosuch) = %v", err)
	}
}
