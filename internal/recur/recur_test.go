package recur

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return l
}

func at(loc *time.Location, y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, loc)
}

func TestRoundTrip(t *testing.T) {
	for _, s := range []string{
		"daily 09:00",
		"weekly mon,wed 08:00",
		"weekly sun,sat 00:00",
		"weekly mon,tue,wed,thu,fri 09:30",
		"monthly 31 10:00",
		"monthly 1 10:00 jalali",
		"every 3d 2026-10-10 09:00",
	} {
		r, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
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

func TestParseWeekdaysNormalised(t *testing.T) {
	r, err := Parse("weekly wed,mon,wed 08:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.String(); got != "weekly mon,wed 08:00" {
		t.Errorf("got %q", got)
	}
}

func TestParseInvalid(t *testing.T) {
	for _, s := range []string{
		"", "hourly 09:00", "daily", "daily 24:00", "daily 09:60", "daily 9:00", "daily 09-00",
		"weekly 09:00", "weekly mon, 08:00", "weekly funday 08:00", "weekly mon,tue",
		"monthly 0 10:00", "monthly 32 10:00", "monthly x 10:00", "monthly 5 10:00 hijri",
		"monthly 5 10:00 gregorian", "monthly 5 25:00 jalali",
		"every 0d 2026-10-10 09:00", "every -1d 2026-10-10 09:00", "every 3 2026-10-10 09:00",
		"every 3d 2026-13-10 09:00", "every 3d 2026-10-10", "every 3d 2026-02-30 09:00",
	} {
		_, err := Parse(s)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalid", s, err)
		}
	}
}

func TestNextBasic(t *testing.T) {
	utc := time.UTC
	tests := []struct {
		rule  string
		after time.Time
		want  time.Time
	}{
		{"daily 09:00", at(utc, 2026, 10, 10, 8, 0), at(utc, 2026, 10, 10, 9, 0)},
		{"daily 09:00", at(utc, 2026, 10, 10, 9, 0), at(utc, 2026, 10, 11, 9, 0)},
		{"daily 09:00", at(utc, 2026, 12, 31, 10, 0), at(utc, 2027, 1, 1, 9, 0)},
		// 2026-10-10 is a Saturday.
		{"weekly mon,wed 08:00", at(utc, 2026, 10, 10, 8, 0), at(utc, 2026, 10, 12, 8, 0)},
		{"weekly mon,wed 08:00", at(utc, 2026, 10, 12, 8, 0), at(utc, 2026, 10, 14, 8, 0)},
		{"weekly sat 08:00", at(utc, 2026, 10, 10, 9, 0), at(utc, 2026, 10, 17, 8, 0)},
		{"monthly 31 10:00", at(utc, 2026, 1, 31, 10, 0), at(utc, 2026, 2, 28, 10, 0)},
		{"monthly 31 10:00", at(utc, 2028, 1, 31, 10, 0), at(utc, 2028, 2, 29, 10, 0)},
		{"monthly 31 10:00", at(utc, 2026, 2, 28, 10, 0), at(utc, 2026, 3, 31, 10, 0)},
		{"monthly 15 10:00", at(utc, 2026, 12, 20, 0, 0), at(utc, 2027, 1, 15, 10, 0)},
		{"every 3d 2026-10-10 09:00", at(utc, 2026, 1, 1, 0, 0), at(utc, 2026, 10, 10, 9, 0)},
		{"every 3d 2026-10-10 09:00", at(utc, 2026, 10, 10, 9, 0), at(utc, 2026, 10, 13, 9, 0)},
		{"every 3d 2026-10-10 09:00", at(utc, 2026, 10, 14, 0, 0), at(utc, 2026, 10, 16, 9, 0)},
		{"every 3d 2026-10-10 09:00", at(utc, 2026, 10, 16, 9, 1), at(utc, 2026, 10, 19, 9, 0)},
		{"every 1d 2026-10-10 09:00", at(utc, 2027, 3, 1, 9, 0), at(utc, 2027, 3, 2, 9, 0)},
	}
	for _, tc := range tests {
		r, err := Parse(tc.rule)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Next(tc.after, utc); !got.Equal(tc.want) {
			t.Errorf("%s after %v = %v, want %v", tc.rule, tc.after, got, tc.want)
		}
	}
}

func TestNextJalali(t *testing.T) {
	loc := time.UTC
	j := func(y, m, d int) time.Time {
		return calendar.Jalali.At(calendar.Date{Year: y, Month: m, Day: d}, 10, 0, loc)
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
		// Mehr has 30 days: the 31st clamps to the 30th.
		{"31st clamps in Mehr", "monthly 31 10:00 jalali", j(1405, 7, 1), j(1405, 7, 30)},
		{"31st in Farvardin stays 31", "monthly 31 10:00 jalali", j(1405, 1, 1), j(1405, 1, 31)},
		{"31st after Shahrivar", "monthly 31 10:00 jalali", j(1405, 6, 31), j(1405, 7, 30)},
		{"1st rolls over", "monthly 1 10:00 jalali", j(1405, 7, 1), j(1405, 8, 1)},
		// 1403 is leap (Esfand 30 exists), 1404 is not.
		{"Esfand 30 leap", "monthly 30 10:00 jalali", j(1403, 12, 1), j(1403, 12, 30)},
		{"Esfand 30 non-leap clamps to 29", "monthly 30 10:00 jalali", j(1404, 12, 1), j(1404, 12, 29)},
		{"after Esfand 29 non-leap", "monthly 30 10:00 jalali", j(1404, 12, 29), j(1405, 1, 30)},
		{"year rollover", "monthly 5 10:00 jalali", j(1404, 12, 10), j(1405, 1, 5)},
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

func TestNextDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	berlin := mustLoc(t, "Europe/Berlin")
	tests := []struct {
		name  string
		rule  string
		loc   *time.Location
		after time.Time
		want  time.Time
	}{
		// Spring forward, NY 2026-03-08: 09:00 stays 09:00 local.
		{"NY spring daily", "daily 09:00", ny, at(ny, 2026, 3, 7, 9, 0), at(ny, 2026, 3, 8, 9, 0)},
		// Gap: 02:30 does not exist on 2026-03-08; Go normalises forward to 03:30 EDT.
		{"NY spring gap", "daily 02:30", ny, at(ny, 2026, 3, 7, 2, 30), at(ny, 2026, 3, 8, 3, 30)},
		// Fall back, NY 2026-11-01: 09:00 stays 09:00 local.
		{"NY fall daily", "daily 09:00", ny, at(ny, 2026, 10, 31, 9, 0), at(ny, 2026, 11, 1, 9, 0)},
		// Berlin: spring forward 2026-03-29, fall back 2026-10-25.
		{"Berlin spring daily", "daily 08:00", berlin, at(berlin, 2026, 3, 28, 8, 0), at(berlin, 2026, 3, 29, 8, 0)},
		{"Berlin spring gap", "daily 02:30", berlin, at(berlin, 2026, 3, 28, 2, 30), at(berlin, 2026, 3, 29, 3, 30)},
		{"Berlin fall daily", "daily 08:00", berlin, at(berlin, 2026, 10, 24, 8, 0), at(berlin, 2026, 10, 25, 8, 0)},
		{"Berlin weekly across fall", "weekly sun 08:00", berlin, at(berlin, 2026, 10, 18, 8, 0), at(berlin, 2026, 10, 25, 8, 0)},
		{"NY every-N across spring", "every 2d 2026-03-06 09:00", ny, at(ny, 2026, 3, 6, 9, 0), at(ny, 2026, 3, 8, 9, 0)},
		{"Berlin monthly across fall", "monthly 25 08:00", berlin, at(berlin, 2026, 9, 25, 8, 0), at(berlin, 2026, 10, 25, 8, 0)},
	}
	for _, tc := range tests {
		r, err := Parse(tc.rule)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Next(tc.after, tc.loc); !got.Equal(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The repeated hour in autumn must fire at most once per local date.
func TestNextRepeatedHourFiresOnce(t *testing.T) {
	for _, tz := range []string{"America/New_York", "Europe/Berlin"} {
		loc := mustLoc(t, tz)
		// NY: 01:30 happens twice on 2026-11-01. Berlin: 02:30 twice on 2026-10-25.
		rule, day := "daily 01:30", at(loc, 2026, 11, 1, 0, 0)
		if tz == "Europe/Berlin" {
			rule, day = "daily 02:30", at(loc, 2026, 10, 25, 0, 0)
		}
		r, err := Parse(rule)
		if err != nil {
			t.Fatal(err)
		}
		first := r.Next(day, loc)
		if first.Day() != day.Day() {
			t.Fatalf("%s: first = %v", tz, first)
		}
		if n := r.Next(first, loc); n.Day() == first.Day() && n.Month() == first.Month() {
			t.Errorf("%s: fired twice on one local date: %v then %v", tz, first, n)
		}
		// Asked from inside the repeated hour (second pass), still not twice.
		later := first.Add(time.Hour)
		if n := r.Next(later, loc); n.Day() == first.Day() && n.Month() == first.Month() {
			t.Errorf("%s: fired again same date: %v", tz, n)
		}
	}
}

func TestNextStrictlyAfterProperty(t *testing.T) {
	rules := []string{
		"daily 09:00", "daily 02:30", "daily 01:30", "weekly mon,wed 08:00", "weekly sun 02:30",
		"monthly 31 10:00", "monthly 29 02:30", "monthly 30 10:00 jalali", "monthly 1 00:00 jalali",
		"every 3d 2026-10-10 09:00", "every 7d 2020-01-01 02:30", "every 400d 2026-10-10 23:59",
	}
	rng := rand.New(rand.NewSource(1))
	base := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	span := int64(10 * 365 * 24 * 3600)
	for _, ls := range []string{"UTC", "America/New_York", "Europe/Berlin", "Asia/Tehran"} {
		loc := mustLoc(t, ls)
		for _, rs := range rules {
			r, err := Parse(rs)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 300; i++ {
				after := time.Unix(base+rng.Int63n(span), 0).In(loc)
				n := r.Next(after, loc)
				if !n.After(after) {
					t.Fatalf("%s %s: Next(%v) = %v not after", ls, rs, after, n)
				}
				if n2 := r.Next(n, loc); !n2.After(n) {
					t.Fatalf("%s %s: Next(Next) = %v not after %v", ls, rs, n2, n)
				}
			}
		}
	}
}

func TestNextInvalidRuleZero(t *testing.T) {
	if got := (Rule{Freq: EveryN}).Next(time.Now(), time.UTC); !got.IsZero() {
		t.Errorf("got %v", got)
	}
}
