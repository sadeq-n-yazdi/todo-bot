package recur

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
)

// Yearly rules (#68).

func TestYearlyRoundTrip(t *testing.T) {
	for _, s := range []string{
		"yearly 01-01 00:00",
		"yearly 02-29 09:00",
		"yearly 12-31 23:59",
		"yearly 07-15 10:00 jalali",
		"yearly 12-30 09:00 jalali",
	} {
		r, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if r.Freq != Yearly {
			t.Fatalf("Parse(%q).Freq = %q", s, r.Freq)
		}
		if got := r.String(); got != s {
			t.Errorf("String() = %q, want %q", got, s)
		}
		r2, err := Parse(r.String())
		if err != nil || !reflect.DeepEqual(r, r2) {
			t.Errorf("round trip of %q: %+v vs %+v (%v)", s, r, r2, err)
		}
	}
}

func TestYearlyParseInvalid(t *testing.T) {
	for _, s := range []string{
		"yearly",
		"yearly 09:00",
		"yearly 02-30 09:00",        // never exists in Gregorian
		"yearly 04-31 09:00",        // never exists
		"yearly 07-31 09:00 jalali", // Mehr has 30 days
		"yearly 12-31 09:00 jalali", // Esfand has at most 30
		"yearly 13-01 09:00",
		"yearly 00-10 09:00",
		"yearly 1-5 09:00",
		"yearly 01-+5 09:00",
		"yearly 01-05 09:00 gregorian",
		"yearly 01-05 25:00",
		"yearly 01-05 09:00 jalali x",
	} {
		if _, err := Parse(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) = %v, want ErrInvalid", s, err)
		}
	}
}

func TestYearlyNextGregorian(t *testing.T) {
	loc := mustLoc(t, "Europe/Berlin")
	tests := []struct {
		name  string
		rule  string
		after time.Time
		want  time.Time
	}{
		{"later this year", "yearly 10-14 09:00", at(loc, 2026, 10, 11, 12, 0), at(loc, 2026, 10, 14, 9, 0)},
		{"same day, before", "yearly 10-14 09:00", at(loc, 2026, 10, 14, 8, 0), at(loc, 2026, 10, 14, 9, 0)},
		{"same day, after", "yearly 10-14 09:00", at(loc, 2026, 10, 14, 9, 0), at(loc, 2027, 10, 14, 9, 0)},
		{"year boundary", "yearly 01-01 00:30", at(loc, 2026, 12, 31, 23, 0), at(loc, 2027, 1, 1, 0, 30)},
		{"29 Feb in a common year", "yearly 02-29 09:00", at(loc, 2026, 1, 1, 0, 0), at(loc, 2026, 2, 28, 9, 0)},
		{"29 Feb in a leap year", "yearly 02-29 09:00", at(loc, 2028, 2, 28, 10, 0), at(loc, 2028, 2, 29, 9, 0)},
		{"29 Feb after the 28th fired", "yearly 02-29 09:00", at(loc, 2027, 2, 28, 9, 0), at(loc, 2028, 2, 29, 9, 0)},
	}
	for _, tc := range tests {
		r, err := Parse(tc.rule)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Next(tc.after, loc); !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestYearlyNextJalali(t *testing.T) {
	loc := time.UTC
	j := func(y, m, d int) time.Time {
		return calendar.Jalali.At(calendar.Date{Year: y, Month: m, Day: d}, 9, 0, loc)
	}
	if !calendar.IsJalaliLeap(1403) || calendar.IsJalaliLeap(1404) {
		t.Fatal("leap assumption broken")
	}
	tests := []struct {
		name  string
		rule  string
		after time.Time
		want  time.Time
	}{
		{"1 Farvardin across the year", "yearly 01-01 09:00 jalali", j(1404, 12, 1), j(1405, 1, 1)},
		{"Mehr 15", "yearly 07-15 09:00 jalali", j(1405, 7, 15), j(1406, 7, 15)},
		{"Esfand 30 leap", "yearly 12-30 09:00 jalali", j(1403, 1, 1), j(1403, 12, 30)},
		{"Esfand 30 common clamps to 29", "yearly 12-30 09:00 jalali", j(1404, 1, 1), j(1404, 12, 29)},
		{"after the clamped day", "yearly 12-30 09:00 jalali", j(1404, 12, 29), j(1405, 12, 29)},
	}
	for _, tc := range tests {
		r, err := Parse(tc.rule)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Next(tc.after, loc); !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
