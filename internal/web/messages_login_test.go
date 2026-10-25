package web

import (
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// The login steps name the Open button of the bot chat and the ⚙ button
// (Settings) of the web app, not the settings of Telegram (#148).
func TestLoginStepsText(t *testing.T) {
	cases := []struct {
		lang i18n.Lang
		want []string
	}{
		{i18n.En, []string{"chat with the bot", "Tap the Open button", "⚙ button (Settings)", "Do not open the settings of Telegram", "Tap Sign in a browser"}},
		{i18n.Fa, []string{"ربات", "«Open»", "⚙", "تنظیمات", "تنظیمات خود تلگرام", "«ورود در مرورگر»"}},
	}
	for _, c := range cases {
		got := i18n.Locale{Lang: c.lang}.T("web.login.steps")
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q does not have %q", c.lang, got, w)
			}
		}
		if strings.Contains(got, "%") {
			t.Errorf("%s: a raw template must not have %%: %q", c.lang, got)
		}
	}
}
