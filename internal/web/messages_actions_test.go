package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

func TestActionsStringsBothLanguages(t *testing.T) {
	verb := regexp.MustCompile(`%[^sd]`)
	n := 0
	for _, k := range i18n.Keys(i18n.En) {
		if !strings.HasPrefix(k, "web.actions.") {
			continue
		}
		n++
		if !hasKey(i18n.Fa, k) {
			t.Errorf("%s has no Farsi string", k)
		}
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			s := (i18n.Locale{Lang: lang}).T(k)
			if verb.MatchString(s) {
				t.Errorf("%s (%s) uses a verb other than %%s and %%d: %q", k, lang, s)
			}
			if strings.ContainsRune(s, '‌') && lang == i18n.En {
				t.Errorf("%s (en) has a zero-width non-joiner", k)
			}
		}
	}
	if n == 0 {
		t.Fatal("no web.actions.* strings")
	}
	for _, k := range i18n.Keys(i18n.Fa) {
		if strings.HasPrefix(k, "web.actions.") && !hasKey(i18n.En, k) {
			t.Errorf("%s has no English string", k)
		}
	}
	// The templates with an argument must keep it in both languages.
	for _, k := range []string{"web.actions.next", "web.actions.reminded"} {
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			if s := (i18n.Locale{Lang: lang}).T(k); strings.Count(s, "%s") != 1 {
				t.Errorf("%s (%s) must have one %%s: %q", k, lang, s)
			}
		}
	}
}
