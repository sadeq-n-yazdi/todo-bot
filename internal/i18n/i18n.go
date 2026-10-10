// Package i18n provides the message catalog (English, Farsi), digit
// localisation and calendar-aware month/weekday names.
//
// Each feature package registers its own strings from an init function in a
// file it owns (e.g. internal/ui/messages.go), so parallel work never edits
// a shared catalog file:
//
//	func init() {
//		i18n.Register(i18n.En, map[string]string{"list.empty": "Nothing to do 🎉"})
//		i18n.Register(i18n.Fa, map[string]string{"list.empty": "کاری نمانده 🎉"})
//	}
package i18n

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
)

// Lang is a supported UI language.
type Lang string

const (
	En Lang = "en"
	Fa Lang = "fa"
)

// Valid reports whether l is supported.
func (l Lang) Valid() bool { return l == En || l == Fa }

// Locale is a user's language and calendar preference.
type Locale struct {
	Lang Lang
	Cal  calendar.Kind
}

// Default picks a locale from a Telegram language_code: "fa*" gives
// Farsi + Jalali, anything else English + Gregorian.
func Default(telegramLangCode string) Locale {
	if strings.HasPrefix(strings.ToLower(telegramLangCode), "fa") {
		return Locale{Fa, calendar.Jalali}
	}
	return Locale{En, calendar.Gregorian}
}

var (
	mu      sync.RWMutex
	catalog = map[Lang]map[string]string{En: {}, Fa: {}}
)

// Register adds messages for lang. It panics on a duplicate key so that
// collisions between packages are caught by any test run.
func Register(lang Lang, msgs map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	m := catalog[lang]
	if m == nil {
		panic("i18n: unknown language " + string(lang))
	}
	for k, v := range msgs {
		if _, dup := m[k]; dup {
			panic("i18n: duplicate key " + k)
		}
		m[k] = v
	}
}

// T formats the message key with fmt.Sprintf-style args. It falls back to
// English, then to the key itself. Digits are NOT localised here (command
// examples must stay ASCII); use Num / Digits for numbers and dates.
func (l Locale) T(key string, args ...any) string {
	mu.RLock()
	s, ok := catalog[l.Lang][key]
	if !ok {
		s, ok = catalog[En][key]
	}
	mu.RUnlock()
	if !ok {
		s = key
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Keys returns all registered keys for lang (for completeness tests).
func Keys(lang Lang) []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(catalog[lang]))
	for k := range catalog[lang] {
		out = append(out, k)
	}
	return out
}

const faDigits = "۰۱۲۳۴۵۶۷۸۹"

// Digits converts ASCII digits in s to the locale's digits (Persian for Fa).
func (l Locale) Digits(s string) string {
	if l.Lang != Fa {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune([]rune(faDigits)[r-'0'])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Num formats n with the locale's digits.
func (l Locale) Num(n int) string { return l.Digits(fmt.Sprint(n)) }

// NormalizeDigits converts Persian (۰-۹) and Arabic-Indic (٠-٩) digits to
// ASCII, and the Arabic comma/Persian separators to ASCII equivalents. Call it on
// all user input before parsing.
func NormalizeDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '۰' && r <= '۹':
			return '0' + (r - '۰')
		case r >= '٠' && r <= '٩':
			return '0' + (r - '٠')
		case r == '٫':
			return '.'
		case r == '،':
			return ','
		}
		return r
	}, s)
}

var monthNames = map[calendar.Kind]map[Lang][12]string{
	calendar.Gregorian: {
		En: {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
		Fa: {"ژانویه", "فوریه", "مارس", "آوریل", "مه", "ژوئن", "ژوئیه", "اوت", "سپتامبر", "اکتبر", "نوامبر", "دسامبر"},
	},
	calendar.Jalali: {
		En: {"Farvardin", "Ordibehesht", "Khordad", "Tir", "Mordad", "Shahrivar", "Mehr", "Aban", "Azar", "Dey", "Bahman", "Esfand"},
		Fa: {"فروردین", "اردیبهشت", "خرداد", "تیر", "مرداد", "شهریور", "مهر", "آبان", "آذر", "دی", "بهمن", "اسفند"},
	},
}

var weekdayNames = map[Lang][7]string{
	En: {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	Fa: {"یکشنبه", "دوشنبه", "سه\u200cشنبه", "چهارشنبه", "پنج\u200cشنبه", "جمعه", "شنبه"}, // \u200c = ZWNJ
}

// MonthName returns the name of month m (1..12) of the locale's calendar.
func (l Locale) MonthName(m int) string { return monthNames[l.cal()][l.lang()][m-1] }

// WeekdayName returns the weekday name; short gives "Mon" in English
// (Farsi has no conventional abbreviations, so the full name is returned).
func (l Locale) WeekdayName(w time.Weekday, short bool) string {
	n := weekdayNames[l.lang()][w]
	if short && l.lang() == En {
		return n[:3]
	}
	return n
}

// MonthNames exposes the month-name table (nlparse matches against it).
func MonthNames(cal calendar.Kind, lang Lang) [12]string { return monthNames[cal][lang] }

// WeekdayNames exposes the weekday-name table.
func WeekdayNames(lang Lang) [7]string { return weekdayNames[lang] }

func (l Locale) lang() Lang {
	if l.Lang.Valid() {
		return l.Lang
	}
	return En
}

func (l Locale) cal() calendar.Kind {
	if l.Cal.Valid() {
		return l.Cal
	}
	return calendar.Gregorian
}
