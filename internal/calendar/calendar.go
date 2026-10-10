// Package calendar converts between the Gregorian and the Jalali (Solar
// Hijri, Iranian) calendars and provides calendar-generic date helpers.
//
// The Jalali conversion is a port of the jalaali-js "breaks" algorithm,
// valid for Jalali years -61..3177.
package calendar

import (
	"fmt"
	"time"
)

// Kind is a calendar system.
type Kind string

const (
	Gregorian Kind = "gregorian"
	Jalali    Kind = "jalali"
)

// Valid reports whether k is a known calendar.
func (k Kind) Valid() bool { return k == Gregorian || k == Jalali }

// Date is a calendar date in some calendar (Month is 1-based).
type Date struct {
	Year, Month, Day int
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// Split returns the date of t (in t's own location) in calendar k.
func (k Kind) Split(t time.Time) Date {
	y, m, d := t.Date()
	if k == Jalali {
		return ToJalali(y, m, d)
	}
	return Date{y, int(m), d}
}

// ToGregorian converts a date of calendar k to a Gregorian civil date.
func (k Kind) ToGregorian(d Date) (int, time.Month, int) {
	if k == Jalali {
		return FromJalali(d.Year, d.Month, d.Day)
	}
	return d.Year, time.Month(d.Month), d.Day
}

// At returns the instant of date d (in calendar k) at hh:mm in loc.
func (k Kind) At(d Date, hh, mm int, loc *time.Location) time.Time {
	y, m, day := k.ToGregorian(d)
	return time.Date(y, m, day, hh, mm, 0, 0, loc)
}

// MonthLength returns the number of days in month m of year y.
func (k Kind) MonthLength(y, m int) int {
	if k == Jalali {
		return JalaliMonthLength(y, m)
	}
	return time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// AddMonths adds n months to (y, m) and returns the resulting year and month.
func AddMonths(y, m, n int) (int, int) {
	t := (y*12 + (m - 1)) + n
	return floorDiv(t, 12), t - floorDiv(t, 12)*12 + 1
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// IsJalaliLeap reports whether Jalali year jy has 366 days.
func IsJalaliLeap(jy int) bool { return jalCal(jy).leap == 0 }

// JalaliMonthLength returns the length of Jalali month jm (1..12) in year jy.
func JalaliMonthLength(jy, jm int) int {
	switch {
	case jm <= 6:
		return 31
	case jm <= 11:
		return 30
	case IsJalaliLeap(jy):
		return 30
	default:
		return 29
	}
}

// ToJalali converts a Gregorian date to Jalali.
func ToJalali(gy int, gm time.Month, gd int) Date { return d2j(g2d(gy, int(gm), gd)) }

// FromJalali converts a Jalali date to Gregorian.
func FromJalali(jy, jm, jd int) (int, time.Month, int) {
	gy, gm, gd := d2g(j2d(jy, jm, jd))
	return gy, time.Month(gm), gd
}

var breaks = [...]int{-61, 9, 38, 199, 426, 686, 756, 818, 1111, 1181, 1210,
	1635, 2060, 2097, 2192, 2262, 2324, 2394, 2456, 3178}

type calInfo struct{ leap, gy, march int }

// jalCal: integer division and remainder truncate toward zero in Go, exactly
// like ~~(a/b) in the original JavaScript.
func jalCal(jy int) calInfo {
	gy := jy + 621
	leapJ := -14
	jp := breaks[0]
	jump := 0
	for i := 1; i < len(breaks); i++ {
		jm := breaks[i]
		jump = jm - jp
		if jy < jm {
			break
		}
		leapJ += jump/33*8 + (jump%33)/4
		jp = jm
	}
	n := jy - jp
	leapJ += n/33*8 + (n%33+3)/4
	if jump%33 == 4 && jump-n == 4 {
		leapJ++
	}
	leapG := gy/4 - (gy/100+1)*3/4 - 150
	march := 20 + leapJ - leapG
	if jump-n < 6 {
		n = n - jump + (jump+4)/33*33
	}
	leap := ((n+1)%33 - 1) % 4
	if leap == -1 {
		leap = 4
	}
	return calInfo{leap: leap, gy: gy, march: march}
}

func j2d(jy, jm, jd int) int {
	r := jalCal(jy)
	return g2d(r.gy, 3, r.march) + (jm-1)*31 - jm/7*(jm-7) + jd - 1
}

func d2j(jdn int) Date {
	gy, _, _ := d2g(jdn)
	jy := gy - 621
	r := jalCal(jy)
	k := jdn - g2d(gy, 3, r.march)
	if k >= 0 {
		if k <= 185 {
			return Date{jy, 1 + k/31, k%31 + 1}
		}
		k -= 186
	} else {
		jy--
		k += 179
		if r.leap == 1 {
			k++
		}
	}
	return Date{jy, 7 + k/30, k%30 + 1}
}

func g2d(gy, gm, gd int) int {
	d := (gy+(gm-8)/6+100100)*1461/4 + (153*((gm+9)%12)+2)/5 + gd - 34840408
	return d - (gy+100100+(gm-8)/6)/100*3/4 + 752
}

func d2g(jdn int) (int, int, int) {
	j := 4*jdn + 139361631
	j += (4*jdn+183187720)/146097*3/4*4 - 3908
	i := (j%1461)/4*5 + 308
	gd := (i%153)/5 + 1
	gm := (i/153)%12 + 1
	gy := j/1461 - 100100 + (8-gm)/6
	return gy, gm, gd
}
