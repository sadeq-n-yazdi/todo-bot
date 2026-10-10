package ui

import (
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

func TestMention(t *testing.T) {
	en := i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}
	fa := i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}
	cases := []struct {
		id   int64
		name string
		lc   i18n.Locale
		want string
	}{
		{5, "wife", en, `<a href="tg://user?id=5">@wife</a>`},
		{5, "wife", fa, `<a href="tg://user?id=5">@wife</a>`},
		{7, "a<b>", en, `<a href="tg://user?id=7">@a&lt;b&gt;</a>`},
		{5, "", en, `<a href="tg://user?id=5">member</a>`},
		{5, "", fa, `<a href="tg://user?id=5">عضو</a>`},
		{0, "wife", en, ""},
	}
	for _, c := range cases {
		if got := Mention(c.id, c.name, c.lc); got != c.want {
			t.Errorf("Mention(%d, %q, %s) = %q, want %q", c.id, c.name, c.lc.Lang, got, c.want)
		}
	}
}
