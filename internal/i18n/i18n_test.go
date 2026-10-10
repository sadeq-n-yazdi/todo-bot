package i18n

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
)

func init() {
	Register(En, map[string]string{"test.hello": "Hello %s", "test.only_en": "only en"})
	Register(Fa, map[string]string{"test.hello": "سلام %s"})
}

func TestT(t *testing.T) {
	fa := Locale{Fa, calendar.Jalali}
	if got := fa.T("test.hello", "x"); got != "سلام x" {
		t.Errorf("got %q", got)
	}
	if got := fa.T("test.only_en"); got != "only en" {
		t.Errorf("fallback: got %q", got)
	}
	if got := fa.T("test.missing"); got != "test.missing" {
		t.Errorf("missing: got %q", got)
	}
}

func TestDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	Register(En, map[string]string{"test.hello": "dup"})
}

func TestDigits(t *testing.T) {
	fa := Locale{Lang: Fa}
	if got := fa.Digits("18:05 1405"); got != "۱۸:۰۵ ۱۴۰۵" {
		t.Errorf("got %q", got)
	}
	if got := NormalizeDigits("۱۸:۰۵ ٣"); got != "18:05 3" {
		t.Errorf("normalize got %q", got)
	}
	if (Locale{Lang: En}).Num(42) != "42" {
		t.Error("en Num")
	}
}

func TestNamesAndDefault(t *testing.T) {
	if got := (Locale{Fa, calendar.Jalali}).MonthName(7); got != "مهر" {
		t.Errorf("got %q", got)
	}
	if got := (Locale{En, calendar.Gregorian}).WeekdayName(time.Monday, true); got != "Mon" {
		t.Errorf("got %q", got)
	}
	if Default("fa-IR") != (Locale{Fa, calendar.Jalali}) || Default("en") != (Locale{En, calendar.Gregorian}) {
		t.Error("Default")
	}
	if (Locale{}).MonthName(1) != "January" {
		t.Error("zero locale should fall back to en/gregorian")
	}
}
