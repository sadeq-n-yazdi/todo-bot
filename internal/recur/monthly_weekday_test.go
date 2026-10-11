package recur

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
)

// Nth and last weekday of the month (#85).

func TestMonthlyWeekdayRoundTrip(t *testing.T) {
	for _, s := range []string{
		"monthly fri#last 16:00",
		"monthly mon#1 09:00",
		"monthly tue#2 00:00",
		"monthly sun#4 23:59",
		"monthly fri#last 16:00 jalali",
		"monthly sat#3 08:30 jalali",
	} {
		r, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if r.Freq != MonthlyWeekday || len(r.Weekdays) != 1 {
			t.Fatalf("Parse(%q) = %+v", s, r)
		}
		if got := r.String(); got != s {
			t.Errorf("String() = %q, want %q", got, s)
		}
		r2, err := Parse(r.String())
		if err != nil || !reflect.DeepEqual(r, r2) {
			t.Errorf("round trip of %q: %+v vs %+v (%v)", s, r, r2, err)
		}
	}
	r, _ := Parse("monthly fri#last 16:00")
	if r.Nth != Last || r.Weekdays[0] != time.Friday || r.Cal != calendar.Gregorian {
		t.Errorf("fields: %+v", r)
	}
}

func TestMonthlyWeekdayParseInvalid(t *testing.T) {
	for _, s := range []string{
		"monthly fri#0 16:00", "monthly fri#5 16:00", "monthly fri#-1 16:00",
		"monthly fri# 16:00", "monthly #1 16:00", "monthly xyz#1 16:00",
		"monthly fri,sat#1 16:00", "monthly fri#1#2 16:00", "monthly fri#LAST 16:00",
		"monthly fri#last", "monthly fri#last 16:00 gregorian", "monthly fri#last 16:00 jalali x",
		"monthly fri#+1 16:00",
	} {
		if _, err := Parse(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalid", s, err)
		}
	}
}

func TestMonthlyWeekdayNextGregorian(t *testing.T) {
	loc := time.UTC
	tests := []struct {
		name  string
		rule  string
		after time.Time
		want  time.Time
	}{
		// October 2026 has five Fridays: 2, 9, 16, 23, 30.
		{"last of five", "monthly fri#last 16:00", at(loc, 2026, 10, 1, 0, 0), at(loc, 2026, 10, 30, 16, 0)},
		{"4th of five", "monthly fri#4 16:00", at(loc, 2026, 10, 1, 0, 0), at(loc, 2026, 10, 23, 16, 0)},
		{"1st", "monthly fri#1 16:00", at(loc, 2026, 10, 1, 0, 0), at(loc, 2026, 10, 2, 16, 0)},
		// November 2026 has four Fridays: 6, 13, 20, 27 (4th == last).
		{"last of four", "monthly fri#last 16:00", at(loc, 2026, 10, 30, 16, 0), at(loc, 2026, 11, 27, 16, 0)},
		{"4th of four", "monthly fri#4 16:00", at(loc, 2026, 10, 23, 16, 0), at(loc, 2026, 11, 27, 16, 0)},
		{"same day before the time", "monthly fri#last 16:00", at(loc, 2026, 10, 30, 15, 59), at(loc, 2026, 10, 30, 16, 0)},
		{"year rollover", "monthly mon#1 09:00", at(loc, 2026, 12, 8, 0, 0), at(loc, 2027, 1, 4, 9, 0)},
		// February 2027 starts on a Monday: Mondays 1, 8, 15, 22.
		{"short month", "monthly mon#last 09:00", at(loc, 2027, 2, 1, 9, 0), at(loc, 2027, 2, 22, 9, 0)},
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

func TestMonthlyWeekdayNextJalali(t *testing.T) {
	loc := time.UTC
	j := func(y, m, d int) time.Time {
		return calendar.Jalali.At(calendar.Date{Year: y, Month: m, Day: d}, 16, 0, loc)
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
		// 1 Aban 1405 is a Friday: Fridays 1, 8, 15, 22, 29.
		{"last Friday is day 29", "monthly fri#last 16:00 jalali", j(1405, 8, 1), j(1405, 8, 29)},
		{"4th Friday of Aban", "monthly fri#4 16:00 jalali", j(1405, 8, 1), j(1405, 8, 22)},
		// 1 Mehr 1405 is a Wednesday: Fridays 3, 10, 17, 24.
		{"follows the Jalali month", "monthly fri#last 16:00 jalali", j(1405, 7, 1), j(1405, 7, 24)},
		// Esfand 1404 (common, 29 days) starts on a Friday.
		{"Esfand common: day 29", "monthly fri#last 16:00 jalali", j(1404, 12, 1), j(1404, 12, 29)},
		// Esfand 1403 (leap, 30 days): Thursdays 2, 9, 16, 23, 30.
		{"Esfand leap: day 30", "monthly thu#last 16:00 jalali", j(1403, 12, 1), j(1403, 12, 30)},
		{"after Esfand into Farvardin", "monthly thu#last 16:00 jalali", j(1403, 12, 30), j(1404, 1, 28)},
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

func TestMonthlyWeekdayDST(t *testing.T) {
	lon := mustLoc(t, "Europe/London")
	// 29 Mar 2026: the last Sunday of March, 01:00–02:00 does not exist.
	r, _ := Parse("monthly sun#last 01:30")
	got := r.Next(time.Date(2026, 3, 1, 0, 0, 0, 0, lon), lon)
	if want := time.Date(2026, 3, 29, 2, 30, 0, 0, lon); !got.Equal(want) {
		t.Errorf("spring gap: got %v, want %v", got, want)
	}
	// 25 Oct 2026: the last Sunday of October, 01:30 happens twice.
	first := r.Next(time.Date(2026, 10, 1, 0, 0, 0, 0, lon), lon)
	if y, m, d := first.Date(); y != 2026 || m != 10 || d != 25 || first.Hour() != 1 {
		t.Fatalf("autumn: got %v", first)
	}
	if again := r.Next(first, lon); again.Month() != time.November {
		t.Errorf("repeated hour fired twice: %v then %v", first, again)
	}
}

// Every occurrence is the right weekday, in the right week of its month, and
// no matching day is skipped.
func TestMonthlyWeekdayProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(85))
	base := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	span := int64(10 * 365 * 24 * 3600)
	for _, cal := range []calendar.Kind{calendar.Gregorian, calendar.Jalali} {
		for _, ls := range []string{"UTC", "Europe/London", "Asia/Tehran"} {
			loc := mustLoc(t, ls)
			for _, nth := range []int{1, 2, 3, 4, Last} {
				for wd := time.Sunday; wd <= time.Saturday; wd++ {
					r := Rule{Freq: MonthlyWeekday, Nth: nth, Weekdays: []time.Weekday{wd}, Cal: cal, Hour: 10}
					if _, err := Parse(r.String()); err != nil {
						t.Fatalf("%s: %v", r, err)
					}
					after := time.Unix(base+rng.Int63n(span), 0).In(loc)
					n := r.Next(after, loc)
					if !n.After(after) || n.Weekday() != wd {
						t.Fatalf("%s %s: Next(%v) = %v", ls, r, after, n)
					}
					cd := cal.Split(n)
					ml := cal.MonthLength(cd.Year, cd.Month)
					if nth == Last && cd.Day+7 <= ml || nth != Last && (cd.Day-1)/7+1 != nth {
						t.Fatalf("%s %s: %v is %s day %d of %d", ls, r, n, cal, cd.Day, ml)
					}
					// No matching date lies strictly between after and n.
					for d := civil(after).AddDate(0, 0, 1); d.Before(civil(n)); d = d.AddDate(0, 0, 1) {
						if r.matches(d, cal) {
							t.Fatalf("%s %s: skipped %v before %v", ls, r, d, n)
						}
					}
				}
			}
		}
	}
}

// civil is the UTC-midnight stand-in for t's local date, as matches expects.
func civil(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestMonthlyWeekdayDSTTehran(t *testing.T) {
	tehran := mustLoc(t, "Asia/Tehran")
	// Until 2022 Iran moved its clocks forward at 00:00 on 1 Farvardin
	// (22 Mar 2019, a Friday), so 00:30 did not exist that day.
	r, _ := Parse("monthly fri#1 00:30 jalali")
	got := r.Next(time.Date(2019, 3, 1, 0, 0, 0, 0, tehran), tehran)
	if want := time.Date(2019, 3, 22, 1, 30, 0, 0, tehran); !got.Equal(want) {
		t.Errorf("spring gap: got %v, want %v", got, want)
	}
}

func TestMonthlyWeekdayInvalidRuleZero(t *testing.T) {
	for _, r := range []Rule{
		{Freq: MonthlyWeekday, Nth: 0, Weekdays: []time.Weekday{time.Friday}},
		{Freq: MonthlyWeekday, Nth: 5, Weekdays: []time.Weekday{time.Friday}},
		{Freq: MonthlyWeekday, Nth: -2, Weekdays: []time.Weekday{time.Friday}},
		{Freq: MonthlyWeekday, Nth: 1},
		{Freq: MonthlyWeekday, Nth: 1, Weekdays: []time.Weekday{time.Monday, time.Friday}},
	} {
		if got := r.Next(time.Now(), time.UTC); !got.IsZero() {
			t.Errorf("%+v: got %v", r, got)
		}
	}
}

// FuzzParse checks that every rule Parse accepts survives String and Parse.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"daily 09:00", "weekly mon,wed 08:00", "monthly 31 10:00", "monthly 1 10:00 jalali",
		"monthly fri#last 16:00", "monthly mon#2 09:00 jalali", "yearly 02-29 09:00",
		"yearly 12-30 09:00 jalali", "every 3d 2026-10-10 09:00",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := Parse(s)
		if err != nil {
			return
		}
		r2, err := Parse(r.String())
		if err != nil || !reflect.DeepEqual(r, r2) {
			t.Fatalf("%q: %+v -> %q -> %+v (%v)", s, r, r.String(), r2, err)
		}
	})
}
