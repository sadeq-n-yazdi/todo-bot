package calendar

import (
	"testing"
	"time"
)

func TestKnownDates(t *testing.T) {
	cases := []struct {
		gy int
		gm time.Month
		gd int
		j  Date
	}{
		{2024, time.March, 20, Date{1403, 1, 1}},
		{2025, time.March, 21, Date{1404, 1, 1}},
		{2023, time.March, 21, Date{1402, 1, 1}},
		{2021, time.March, 20, Date{1399, 12, 30}}, // 1399 is leap
		{2026, time.October, 10, Date{1405, 7, 18}},
		{1979, time.February, 11, Date{1357, 11, 22}},
		{2000, time.January, 1, Date{1378, 10, 11}},
	}
	for _, c := range cases {
		if got := ToJalali(c.gy, c.gm, c.gd); got != c.j {
			t.Errorf("ToJalali(%d-%d-%d) = %v, want %v", c.gy, c.gm, c.gd, got, c.j)
		}
		gy, gm, gd := FromJalali(c.j.Year, c.j.Month, c.j.Day)
		if gy != c.gy || gm != c.gm || gd != c.gd {
			t.Errorf("FromJalali(%v) = %d-%d-%d", c.j, gy, gm, gd)
		}
	}
}

func TestRoundTripEveryDay(t *testing.T) {
	d := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := ToJalali(d.Date())
	for d = d.AddDate(0, 0, 1); d.Before(end); d = d.AddDate(0, 0, 1) {
		j := ToJalali(d.Date())
		gy, gm, gd := FromJalali(j.Year, j.Month, j.Day)
		if y, m, dd := d.Date(); gy != y || gm != m || gd != dd {
			t.Fatalf("round trip %v -> %v -> %d-%d-%d", d, j, gy, gm, gd)
		}
		// consecutive days must be consecutive Jalali dates
		if j.Day != 1 && j.Day != prev.Day+1 {
			t.Fatalf("non-consecutive %v after %v", j, prev)
		}
		if j.Day == 1 && prev.Day != JalaliMonthLength(prev.Year, prev.Month) {
			t.Fatalf("month %v ended at day %d, length %d", prev, prev.Day, JalaliMonthLength(prev.Year, prev.Month))
		}
		prev = j
	}
}

func TestLeapYears(t *testing.T) {
	for _, y := range []int{1399, 1403, 1408} {
		if !IsJalaliLeap(y) {
			t.Errorf("%d should be leap", y)
		}
	}
	for _, y := range []int{1400, 1401, 1402, 1404} {
		if IsJalaliLeap(y) {
			t.Errorf("%d should not be leap", y)
		}
	}
}

func TestKindHelpers(t *testing.T) {
	tm := time.Date(2026, 10, 10, 23, 30, 0, 0, time.UTC)
	if got := Jalali.Split(tm); got != (Date{1405, 7, 18}) {
		t.Fatalf("Split = %v", got)
	}
	at := Jalali.At(Date{1405, 1, 1}, 9, 0, time.UTC)
	if !at.Equal(time.Date(2026, 3, 21, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("At = %v", at)
	}
	if Gregorian.MonthLength(2028, 2) != 29 || Jalali.MonthLength(1404, 12) != 29 {
		t.Fatal("MonthLength")
	}
	if y, m := AddMonths(1405, 12, 1); y != 1406 || m != 1 {
		t.Fatalf("AddMonths = %d,%d", y, m)
	}
	if y, m := AddMonths(1405, 1, -1); y != 1404 || m != 12 {
		t.Fatalf("AddMonths neg = %d,%d", y, m)
	}
}
