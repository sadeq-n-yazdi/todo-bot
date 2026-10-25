package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

func TestTabsStringsBothLanguages(t *testing.T) {
	verb := regexp.MustCompile(`%[^sd]`)
	n := 0
	for _, k := range i18n.Keys(i18n.En) {
		if !strings.HasPrefix(k, "web.tabs.") {
			continue
		}
		n++
		if !hasKey(i18n.Fa, k) {
			t.Errorf("%s has no Farsi string", k)
		}
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			if s := (i18n.Locale{Lang: lang}).T(k); verb.MatchString(s) {
				t.Errorf("%s (%s) uses a verb other than %%s and %%d: %q", k, lang, s)
			}
		}
	}
	if n == 0 {
		t.Fatal("no web.tabs.* strings")
	}
	for _, k := range i18n.Keys(i18n.Fa) {
		if strings.HasPrefix(k, "web.tabs.") && !hasKey(i18n.En, k) {
			t.Errorf("%s has no English string", k)
		}
	}
}
