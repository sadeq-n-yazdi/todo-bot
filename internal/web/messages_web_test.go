package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

func TestEveryErrorHasMessages(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range allErrors {
		seen[e.Code] = true
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			if !hasKey(lang, "web.err."+e.Code) {
				t.Errorf("no %s string for web.err.%s", lang, e.Code)
			}
		}
	}
	for _, k := range i18n.Keys(i18n.En) {
		if c, ok := strings.CutPrefix(k, "web.err."); ok && !seen[c] {
			t.Errorf("web.err.%s has no error code", c)
		}
	}
}

func hasKey(lang i18n.Lang, key string) bool {
	for _, k := range i18n.Keys(lang) {
		if k == key {
			return true
		}
	}
	return false
}

func TestWebStringsBothLanguages(t *testing.T) {
	verb := regexp.MustCompile(`%[^sd]`)
	for _, k := range i18n.Keys(i18n.En) {
		if !strings.HasPrefix(k, "web.app.") && !strings.HasPrefix(k, "web.err.") {
			continue
		}
		if !hasKey(i18n.Fa, k) {
			t.Errorf("%s has no Farsi string", k)
		}
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			if s := (i18n.Locale{Lang: lang}).T(k); verb.MatchString(s) {
				t.Errorf("%s (%s) uses a verb other than %%s and %%d: %q", k, lang, s)
			}
		}
	}
	for _, k := range []string{"web.app.notallowed", "web.app.open_in_telegram", "web.app.error"} {
		if !hasKey(i18n.En, k) {
			t.Errorf("the front end needs %s", k)
		}
	}
}
