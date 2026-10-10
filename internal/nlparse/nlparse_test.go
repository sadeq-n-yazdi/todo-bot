package nlparse

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

const layout = "2006-01-02 15:04"

var (
	greg = calendar.Gregorian
	jal  = calendar.Jalali
)

func loc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata for %s unavailable: %v", name, err)
	}
	return l
}

func wk(days ...time.Weekday) []time.Weekday { return days }

func daily(h, m int) *recur.Rule {
	return &recur.Rule{Freq: recur.Daily, Cal: greg, Hour: h, Minute: m}
}

func weekly(h, m int, days ...time.Weekday) *recur.Rule {
	return &recur.Rule{Freq: recur.Weekly, Cal: greg, Weekdays: days, Hour: h, Minute: m}
}

func monthly(cal calendar.Kind, day, h, m int) *recur.Rule {
	return &recur.Rule{Freq: recur.Monthly, Cal: cal, MonthDay: day, Hour: h, Minute: m}
}

type row struct {
	in       string
	tz       string // default Asia/Tehran
	now      string // default Saturday 2026-10-10 08:15 (18 Mehr 1405)
	cal      calendar.Kind
	wantText string
	wantDue  string // "" = no date
	wantTime bool
	wantRule *recur.Rule
}

var monFri = wk(time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)

var rows = []row{
	// ---- English: days and times ----
	{in: "buy milk tomorrow", wantText: "buy milk", wantDue: "2026-10-11 09:00"},
	{in: "buy milk tomorrow 18:00", wantText: "buy milk", wantDue: "2026-10-11 18:00", wantTime: true},
	{in: "buy milk Tomorrow at 6:30pm", wantText: "buy milk", wantDue: "2026-10-11 18:30", wantTime: true},
	{in: "call mom today", wantText: "call mom", wantDue: "2026-10-10 09:00"},
	{in: "call mom today 10:00", wantText: "call mom", wantDue: "2026-10-10 10:00", wantTime: true},
	{in: "walk tonight", wantText: "walk", wantDue: "2026-10-10 20:00", wantTime: true},
	{in: "walk tonight at 9", wantText: "walk", wantDue: "2026-10-10 21:00", wantTime: true},
	{in: "x day after tomorrow", wantText: "x", wantDue: "2026-10-12 09:00"},
	{in: "stand-up at 18:00", wantText: "stand-up", wantDue: "2026-10-10 18:00", wantTime: true},
	{in: "stand-up 18:00", wantText: "stand-up", wantDue: "2026-10-10 18:00", wantTime: true},
	{in: "pills 9am", wantText: "pills", wantDue: "2026-10-10 09:00", wantTime: true},
	{in: "pills 9:30pm", wantText: "pills", wantDue: "2026-10-10 21:30", wantTime: true},
	{in: "pills 12am", wantText: "pills", wantDue: "2026-10-11 00:00", wantTime: true},
	{in: "pills 12 pm", wantText: "pills", wantDue: "2026-10-10 12:00", wantTime: true},
	{in: "pills 8am", wantText: "pills", wantDue: "2026-10-11 08:00", wantTime: true}, // already passed: tomorrow
	{in: "pills at 5", wantText: "pills", wantDue: "2026-10-11 05:00", wantTime: true},
	{in: "pills at 17", wantText: "pills", wantDue: "2026-10-10 17:00", wantTime: true},
	{in: "tomorrow", wantText: "", wantDue: "2026-10-11 09:00"},
	{in: "  buy milk   tomorrow  ", wantText: "buy milk", wantDue: "2026-10-11 09:00"},
	{in: "buy milk, tomorrow", wantText: "buy milk", wantDue: "2026-10-11 09:00"},
	// ---- English: relative ----
	{in: "stretch in 2h", wantText: "stretch", wantDue: "2026-10-10 10:15", wantTime: true},
	{in: "stretch in 10m", wantText: "stretch", wantDue: "2026-10-10 08:25", wantTime: true},
	{in: "stretch in 2 hours", wantText: "stretch", wantDue: "2026-10-10 10:15", wantTime: true},
	{in: "stretch in 45 minutes", wantText: "stretch", wantDue: "2026-10-10 09:00", wantTime: true},
	{in: "stretch in 3 days", wantText: "stretch", wantDue: "2026-10-13 09:00"},
	{in: "stretch in 3 days at 14:00", wantText: "stretch", wantDue: "2026-10-13 14:00", wantTime: true},
	{in: "stretch in 2 weeks", wantText: "stretch", wantDue: "2026-10-24 09:00"},
	// ---- English: weekdays ----
	{in: "gym next monday", wantText: "gym", wantDue: "2026-10-12 09:00"},
	{in: "gym on friday at 14:30", wantText: "gym", wantDue: "2026-10-16 14:30", wantTime: true},
	{in: "gym on mon", wantText: "gym", wantDue: "2026-10-12 09:00"},
	{in: "gym on tue", wantText: "gym", wantDue: "2026-10-13 09:00"},
	{in: "gym on wed", wantText: "gym", wantDue: "2026-10-14 09:00"},
	{in: "gym on thu", wantText: "gym", wantDue: "2026-10-15 09:00"},
	{in: "gym on fri", wantText: "gym", wantDue: "2026-10-16 09:00"},
	{in: "gym on sat", wantText: "gym", wantDue: "2026-10-10 09:00"},
	{in: "gym on sun", wantText: "gym", wantDue: "2026-10-11 09:00"},
	{in: "gym next saturday", wantText: "gym", wantDue: "2026-10-17 09:00"},
	{in: "gym saturday 7am", wantText: "gym", wantDue: "2026-10-17 07:00", wantTime: true},
	{in: "18:00 friday", wantText: "", wantDue: "2026-10-16 18:00", wantTime: true},
	// ---- English: dates ----
	{in: "tax 15 Oct", wantText: "tax", wantDue: "2026-10-15 09:00"},
	{in: "tax Oct 15", wantText: "tax", wantDue: "2026-10-15 09:00"},
	{in: "tax 15 October 2027", wantText: "tax", wantDue: "2027-10-15 09:00"},
	{in: "tax Oct 15th 2027 8:00", wantText: "tax", wantDue: "2027-10-15 08:00", wantTime: true},
	{in: "tax on 3 Jan", wantText: "tax", wantDue: "2027-01-03 09:00"}, // already past this year
	{in: "tax 2026-10-15", wantText: "tax", wantDue: "2026-10-15 09:00"},
	{in: "tax 2026-10-15 17:45", wantText: "tax", wantDue: "2026-10-15 17:45", wantTime: true},
	{in: "tax 15/10", wantText: "tax", wantDue: "2026-10-15 09:00"},
	{in: "tax 15/10/2026", wantText: "tax", wantDue: "2026-10-15 09:00"},
	{in: "tax 15/10/27", wantText: "tax", wantDue: "2027-10-15 09:00"},
	{in: "tax 2026-10-15", cal: jal, wantText: "tax", wantDue: "2026-10-15 09:00"}, // ISO date is Gregorian in any calendar
	{in: "tax 15 Oct", cal: jal, wantText: "tax", wantDue: "2026-10-15 09:00"},     // Gregorian name, Jalali user
	{in: "leap 29 Feb", wantText: "leap", wantDue: "2028-02-29 09:00"},
	{in: "bad 31/2", wantText: "bad 31/2"},
	// ---- English: Jalali ----
	{in: "tax 25 Mehr", wantText: "tax", wantDue: "2026-10-17 09:00"},
	{in: "tax 15 Mehr", wantText: "tax", wantDue: "2027-10-07 09:00"}, // 15 Mehr 1405 has passed
	{in: "tax Mehr 25", cal: jal, wantText: "tax", wantDue: "2026-10-17 09:00"},
	{in: "tax 15 Mehr 1405", wantText: "tax", wantDue: "2026-10-07 09:00"},
	{in: "tax 1405/07/25", cal: jal, wantText: "tax", wantDue: "2026-10-17 09:00"},
	{in: "tax 1405/07/25 18:00", cal: jal, wantText: "tax", wantDue: "2026-10-17 18:00", wantTime: true},
	{in: "tax 25/7", cal: jal, wantText: "tax", wantDue: "2026-10-17 09:00"},
	{in: "tax 25/10", cal: greg, wantText: "tax", wantDue: "2026-10-25 09:00"},
	{in: "tax 25/7/1405", cal: jal, wantText: "tax", wantDue: "2026-10-17 09:00"},
	// ---- English: recurrence ----
	{in: "pills every day 9am", wantText: "pills", wantDue: "2026-10-10 09:00", wantTime: true, wantRule: daily(9, 0)},
	{in: "pills daily 9am", wantText: "pills", wantDue: "2026-10-10 09:00", wantTime: true, wantRule: daily(9, 0)},
	{in: "pills daily at 7:15", wantText: "pills", wantDue: "2026-10-11 07:15", wantTime: true, wantRule: daily(7, 15)},
	{in: "pills everyday 21:00", wantText: "pills", wantDue: "2026-10-10 21:00", wantTime: true, wantRule: daily(21, 0)},
	{in: "report weekdays 9:30", wantText: "report", wantDue: "2026-10-12 09:30", wantTime: true, wantRule: weekly(9, 30, monFri...)},
	{in: "run every monday and wednesday 8:00", wantText: "run", wantDue: "2026-10-12 08:00", wantTime: true,
		wantRule: weekly(8, 0, time.Monday, time.Wednesday)},
	{in: "run every wed, mon 8:00", wantText: "run", wantDue: "2026-10-12 08:00", wantTime: true,
		wantRule: weekly(8, 0, time.Monday, time.Wednesday)},
	{in: "run every week on tue", wantText: "run", wantDue: "2026-10-13 09:00", wantRule: weekly(9, 0, time.Tuesday)},
	{in: "run weekly on sat", wantText: "run", wantDue: "2026-10-10 09:00", wantRule: weekly(9, 0, time.Saturday)},
	{in: "run every week", wantText: "run", wantDue: "2026-10-10 09:00", wantRule: weekly(9, 0, time.Saturday)},
	{in: "run every friday", wantText: "run", wantDue: "2026-10-16 09:00", wantRule: weekly(9, 0, time.Friday)},
	{in: "rent every month on 31st", wantText: "rent", wantDue: "2026-10-31 09:00", wantRule: monthly(greg, 31, 9, 0)},
	{in: "rent monthly on the 1st", wantText: "rent", wantDue: "2026-11-01 09:00", wantRule: monthly(greg, 1, 9, 0)},
	{in: "rent monthly on the 1st at 10:00", wantText: "rent", wantDue: "2026-11-01 10:00", wantTime: true, wantRule: monthly(greg, 1, 10, 0)},
	{in: "rent every month on the 10th", wantText: "rent", wantDue: "2026-10-10 09:00", wantRule: monthly(greg, 10, 9, 0)},
	{in: "rent every month", wantText: "rent", wantDue: "2026-10-10 09:00", wantRule: monthly(greg, 10, 9, 0)},
	{in: "rent every month on 31st", cal: jal, wantText: "rent", wantDue: "2026-10-22 09:00", wantRule: monthly(jal, 31, 9, 0)}, // Mehr has 30 days
	{in: "rent monthly on the 1st", cal: jal, wantText: "rent", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)},
	{in: "water plants every 3 days", wantText: "water plants", wantDue: "2026-10-10 09:00",
		wantRule: &recur.Rule{Freq: recur.EveryN, Interval: 3, Cal: greg, Hour: 9, Anchor: calendar.Date{Year: 2026, Month: 10, Day: 10}}},
	{in: "water plants every 2 days 7:00", wantText: "water plants", wantDue: "2026-10-11 07:00", wantTime: true,
		wantRule: &recur.Rule{Freq: recur.EveryN, Interval: 2, Cal: greg, Hour: 7, Anchor: calendar.Date{Year: 2026, Month: 10, Day: 11}}},
	{in: "water plants every 1 day", wantText: "water plants", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},

	// ---- Farsi: days and times ----
	{in: "خرید نان امروز", wantText: "خرید نان", wantDue: "2026-10-10 09:00"},
	{in: "خرید نان فردا", wantText: "خرید نان", wantDue: "2026-10-11 09:00"},
	{in: "خرید نان پس\u200cفردا", wantText: "خرید نان", wantDue: "2026-10-12 09:00"},
	{in: "خرید نان پس فردا", wantText: "خرید نان", wantDue: "2026-10-12 09:00"},
	{in: "خرید نان امشب", wantText: "خرید نان", wantDue: "2026-10-10 20:00", wantTime: true},
	{in: "خرید نان فردا ساعت ۱۸", wantText: "خرید نان", wantDue: "2026-10-11 18:00", wantTime: true},
	{in: "خرید نان ساعت ۱۸", wantText: "خرید نان", wantDue: "2026-10-10 18:00", wantTime: true},
	{in: "خرید نان ساعت ۹:۳۰", wantText: "خرید نان", wantDue: "2026-10-10 09:30", wantTime: true},
	{in: "خرید نان ۹ صبح", wantText: "خرید نان", wantDue: "2026-10-10 09:00", wantTime: true},
	{in: "خرید نان ساعت ۹ صبح", wantText: "خرید نان", wantDue: "2026-10-10 09:00", wantTime: true},
	{in: "خرید نان ۵ عصر", wantText: "خرید نان", wantDue: "2026-10-10 17:00", wantTime: true},
	{in: "خرید نان ۸ شب", wantText: "خرید نان", wantDue: "2026-10-10 20:00", wantTime: true},
	{in: "خرید نان ۱۲ ظهر", wantText: "خرید نان", wantDue: "2026-10-10 12:00", wantTime: true},
	{in: "خرید نان ساعت 18", wantText: "خرید نان", wantDue: "2026-10-10 18:00", wantTime: true},
	{in: "خرید نان ساعت ٦", wantText: "خرید نان", wantDue: "2026-10-11 06:00", wantTime: true}, // Arabic-Indic digit
	// ---- Farsi: relative ----
	{in: "استراحت ۲ ساعت دیگر", wantText: "استراحت", wantDue: "2026-10-10 10:15", wantTime: true},
	{in: "استراحت ۱۰ دقیقه دیگر", wantText: "استراحت", wantDue: "2026-10-10 08:25", wantTime: true},
	{in: "استراحت ۳ روز دیگر", wantText: "استراحت", wantDue: "2026-10-13 09:00"},
	// ---- Farsi: weekdays ----
	{in: "باشگاه شنبه", wantText: "باشگاه", wantDue: "2026-10-10 09:00"},
	{in: "باشگاه یکشنبه", wantText: "باشگاه", wantDue: "2026-10-11 09:00"},
	{in: "باشگاه دوشنبه", wantText: "باشگاه", wantDue: "2026-10-12 09:00"},
	{in: "باشگاه سه\u200cشنبه", wantText: "باشگاه", wantDue: "2026-10-13 09:00"},
	{in: "باشگاه سهشنبه", wantText: "باشگاه", wantDue: "2026-10-13 09:00"},
	{in: "باشگاه سه شنبه", wantText: "باشگاه", wantDue: "2026-10-13 09:00"},
	{in: "باشگاه چهارشنبه", wantText: "باشگاه", wantDue: "2026-10-14 09:00"},
	{in: "باشگاه پنج\u200cشنبه", wantText: "باشگاه", wantDue: "2026-10-15 09:00"},
	{in: "باشگاه پنجشنبه", wantText: "باشگاه", wantDue: "2026-10-15 09:00"},
	{in: "باشگاه پنج شنبه ساعت ۱۷", wantText: "باشگاه", wantDue: "2026-10-15 17:00", wantTime: true},
	{in: "باشگاه جمعه", wantText: "باشگاه", wantDue: "2026-10-16 09:00"},
	{in: "باشگاه شنبه بعد", wantText: "باشگاه", wantDue: "2026-10-17 09:00"},
	// ---- Farsi: dates ----
	{in: "مالیات ۲۵ مهر", cal: jal, wantText: "مالیات", wantDue: "2026-10-17 09:00"},
	{in: "مالیات ۲۵ مهر", cal: greg, wantText: "مالیات", wantDue: "2026-10-17 09:00"},
	{in: "مالیات ۱۵ مهر", cal: jal, wantText: "مالیات", wantDue: "2027-10-07 09:00"},
	{in: "مالیات ۱۵ مهر ۱۴۰۵", cal: jal, wantText: "مالیات", wantDue: "2026-10-07 09:00"},
	{in: "مالیات ۱ فروردین ۱۴۰۶ ساعت ۸", cal: jal, wantText: "مالیات", wantDue: "2027-03-21 08:00", wantTime: true},
	{in: "مالیات ۱۴۰۵/۰۷/۲۵", cal: jal, wantText: "مالیات", wantDue: "2026-10-17 09:00"},
	{in: "مالیات ۲۵/۷", cal: jal, wantText: "مالیات", wantDue: "2026-10-17 09:00"},
	{in: "مالیات ۱۵ اکتبر", cal: jal, wantText: "مالیات", wantDue: "2026-10-15 09:00"}, // Gregorian name, Jalali user
	// ---- Farsi: recurrence ----
	{in: "قرص هر روز", wantText: "قرص", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
	{in: "قرص هر روز ساعت ۷", wantText: "قرص", wantDue: "2026-10-11 07:00", wantTime: true, wantRule: daily(7, 0)},
	{in: "ورزش هر هفته دوشنبه", wantText: "ورزش", wantDue: "2026-10-12 09:00", wantRule: weekly(9, 0, time.Monday)},
	{in: "ورزش هر دوشنبه و چهارشنبه", wantText: "ورزش", wantDue: "2026-10-12 09:00", wantRule: weekly(9, 0, time.Monday, time.Wednesday)},
	{in: "ورزش هر سه\u200cشنبه ۸ صبح", wantText: "ورزش", wantDue: "2026-10-13 08:00", wantTime: true, wantRule: weekly(8, 0, time.Tuesday)},
	{in: "اجاره هر ماه ۱ام", cal: greg, wantText: "اجاره", wantDue: "2026-11-01 09:00", wantRule: monthly(greg, 1, 9, 0)},
	{in: "اجاره هر ماه ۱ام", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)},
	{in: "اجاره هر ماه روز ۱", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)},
	{in: "اجاره هر ماه روز ۱ ساعت ۱۰", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 10:00", wantTime: true, wantRule: monthly(jal, 1, 10, 0)},
	{in: "آب دادن هر ۳ روز", wantText: "آب دادن", wantDue: "2026-10-10 09:00",
		wantRule: &recur.Rule{Freq: recur.EveryN, Interval: 3, Cal: greg, Hour: 9, Anchor: calendar.Date{Year: 2026, Month: 10, Day: 10}}},
	{in: "گزارش روزهای کاری", wantText: "گزارش", wantDue: "2026-10-10 09:00",
		wantRule: weekly(9, 0, time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Saturday)},
	{in: "گزارش روزهای کاری ساعت ۸:۳۰", wantText: "گزارش", wantDue: "2026-10-10 08:30", wantTime: true,
		wantRule: weekly(8, 30, time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Saturday)},

	// ---- false positives: text kept whole, no date ----
	{in: "fix 2 bugs", wantText: "fix 2 bugs"},
	{in: "monday meeting notes", wantText: "monday meeting notes"},
	{in: "call 911", wantText: "call 911"},
	{in: "خرید ۲ نان", wantText: "خرید ۲ نان"},
	{in: "buy milk", wantText: "buy milk"},
	{in: "read chapter 5", wantText: "read chapter 5"},
	{in: "ساعت", wantText: "ساعت"},
	{in: "every", wantText: "every"},
	{in: "meet at", wantText: "meet at"},
	{in: "", wantText: ""},
	{in: "   ", wantText: ""},
	{in: "pills 25:00", wantText: "pills 25:00"},
	{in: "pills 13pm", wantText: "pills 13pm"},

	// ---- other zones, DST, defaults ----
	{in: "call tomorrow", tz: "America/New_York", now: "2026-11-01 08:00", wantText: "call", wantDue: "2026-11-02 09:00"},
	{in: "call in 2 days at 9", tz: "America/New_York", now: "2026-11-01 08:00", wantText: "call", wantDue: "2026-11-03 09:00", wantTime: true},
	{in: "pills every day 9am", tz: "America/New_York", now: "2026-11-01 08:00", wantText: "pills", wantDue: "2026-11-01 09:00", wantTime: true, wantRule: daily(9, 0)},
	{in: "x tomorrow", tz: "Europe/London", now: "2026-03-28 22:00", wantText: "x", wantDue: "2026-03-29 09:00"},
	{in: "x every month", now: "2026-10-31 08:00", cal: greg, wantText: "x", wantDue: "2026-10-31 09:00", wantRule: monthly(greg, 31, 9, 0)},
	// ---- review fixes ----
	{in: "stretch in 2h", tz: "America/New_York", now: "2026-03-08 01:30", wantText: "stretch", wantDue: "2026-03-08 04:30", wantTime: true},
	{in: "stretch in 90m", tz: "America/New_York", now: "2026-03-08 01:30", wantText: "stretch", wantDue: "2026-03-08 04:00", wantTime: true},
	{in: "stretch in 2h", tz: "America/New_York", now: "2026-11-01 00:30", wantText: "stretch", wantDue: "2026-11-01 01:30", wantTime: true},
	{in: "x 10 Oct", now: "2026-10-10 15:00", wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x 10/10", now: "2026-10-10 15:00", wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x 18 Mehr", now: "2026-10-10 15:00", cal: jal, wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x today", now: "2026-10-10 15:00", wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x امروز", now: "2026-10-10 15:00", wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x tonight", now: "2026-10-10 21:00", wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x امشب", now: "2026-10-10 21:00", wantText: "x", wantDue: "2026-10-10 23:59"},
	{in: "x today 9am", now: "2026-10-10 15:00", wantText: "x", wantDue: "2026-10-10 09:00", wantTime: true},
	{in: "x 9am", now: "2026-10-10 15:00", wantText: "x", wantDue: "2026-10-11 09:00", wantTime: true},
	{in: "x tonight 9am", wantText: "x", wantDue: "2026-10-11 09:00", wantTime: true},
	{in: "x tonight at 9", wantText: "x", wantDue: "2026-10-10 21:00", wantTime: true},
	{in: "x tonight at 3", wantText: "x", wantDue: "2026-10-10 15:00", wantTime: true},
	{in: "x tonight at 3", now: "2026-10-10 16:00", wantText: "x", wantDue: "2026-10-10 03:00", wantTime: true},
	{in: "خرید نان ساعت۱۸", wantText: "خرید نان", wantDue: "2026-10-10 18:00", wantTime: true},
	{in: "مالیات ۲۵مین مهر", cal: jal, wantText: "مالیات", wantDue: "2026-10-17 09:00"},
	{in: "مالیات ۲۵ ام مهر", cal: jal, wantText: "مالیات", wantDue: "2026-10-17 09:00"},
	{in: "اجاره هر ماه ۱مین", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)},
	{in: "اجاره هر ماه ۱ ام", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)},
	{in: "خريد نان فردا", wantText: "خريد نان", wantDue: "2026-10-11 09:00"},
	{in: "باشگاه يكشنبه", wantText: "باشگاه", wantDue: "2026-10-11 09:00"},
	{in: "feed the cat sat 9am", wantText: "feed the cat", wantDue: "2026-10-10 09:00", wantTime: true},
	{in: "run weekly on sat", wantText: "run", wantDue: "2026-10-10 09:00", wantRule: weekly(9, 0, time.Saturday)},
	{in: "run next sat", wantText: "run", wantDue: "2026-10-17 09:00"},
	{in: "تماس با ۱۱۰", wantText: "تماس با ۱۱۰"},
	{in: "کتاب ۱۰۰ سال تنهایی", wantText: "کتاب ۱۰۰ سال تنهایی"},
	{in: "meeting with Sam on project", wantText: "meeting with Sam on project"},
	{in: "pay 3 bills", wantText: "pay 3 bills"},
	{in: "watch the sun", wantText: "watch the sun"},
	{in: "feed the cat sat", wantText: "feed the cat sat"},
	{in: "send weekly", wantText: "send weekly"},
	{in: "pills daily", wantText: "pills daily"},
}

func parseNow(t *testing.T, s, tz string) time.Time {
	t.Helper()
	if tz == "" {
		tz = "Asia/Tehran"
	}
	if s == "" {
		s = "2026-10-10 08:15"
	}
	now, err := time.ParseInLocation(layout, s, loc(t, tz))
	if err != nil {
		t.Fatal(err)
	}
	return now
}

func TestParse(t *testing.T) {
	for _, r := range rows {
		cal := r.cal
		if cal == "" {
			cal = greg
		}
		t.Run(r.in+"|"+string(cal), func(t *testing.T) {
			now := parseNow(t, r.now, r.tz)
			got, err := Parser{}.Parse(r.in, now, cal)
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != r.wantText {
				t.Errorf("Text = %q, want %q", got.Text, r.wantText)
			}
			if r.wantDue == "" {
				if got.Due != nil || got.Rule != nil || got.HasTime {
					t.Errorf("want no date, got due=%v rule=%+v hasTime=%v", got.Due, got.Rule, got.HasTime)
				}
				return
			}
			if got.Due == nil {
				t.Fatalf("Due = nil, want %s", r.wantDue)
			}
			if s := got.Due.Format(layout); s != r.wantDue {
				t.Errorf("Due = %s, want %s", s, r.wantDue)
			}
			if got.Due.Location() != now.Location() {
				t.Errorf("Due location = %v, want %v", got.Due.Location(), now.Location())
			}
			if got.HasTime != r.wantTime {
				t.Errorf("HasTime = %v, want %v", got.HasTime, r.wantTime)
			}
			if !reflect.DeepEqual(got.Rule, r.wantRule) {
				t.Errorf("Rule = %+v, want %+v", got.Rule, r.wantRule)
			}
		})
	}
}

func TestParseDefaultTime(t *testing.T) {
	now := parseNow(t, "", "")
	got, err := Parser{DefaultHour: 7, DefaultMinute: 45}.Parse("x tomorrow", now, greg)
	if err != nil || got.Due == nil || got.Due.Format(layout) != "2026-10-11 07:45" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, _ = Parser{DefaultHour: 7, DefaultMinute: 45}.Parse("x every day", now, greg)
	if got.Rule == nil || got.Rule.Hour != 7 || got.Rule.Minute != 45 {
		t.Fatalf("rule = %+v", got.Rule)
	}
	got, _ = Parser{DefaultMinute: 30}.Parse("x tomorrow", now, greg)
	if got.Due.Format(layout) != "2026-10-11 00:30" {
		t.Fatalf("due = %v", got.Due)
	}
}

func TestParseWhen(t *testing.T) {
	now := parseNow(t, "", "")
	tests := []struct {
		in       string
		cal      calendar.Kind
		wantDue  string
		wantRule *recur.Rule
		wantErr  bool
	}{
		{in: "tomorrow 18:00", wantDue: "2026-10-11 18:00"},
		{in: "فردا ساعت ۱۸", wantDue: "2026-10-11 18:00"},
		{in: "in 2 hours", wantDue: "2026-10-10 10:15"},
		{in: "18:00", wantDue: "2026-10-10 18:00"},
		{in: "۲۵ مهر", cal: jal, wantDue: "2026-10-17 09:00"},
		{in: "every day 9am", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
		{in: "هر هفته دوشنبه", wantDue: "2026-10-12 09:00", wantRule: weekly(9, 0, time.Monday)},
		{in: "buy milk tomorrow", wantErr: true},
		{in: "tomorrow buy milk", wantErr: true},
		{in: "fix 2 bugs", wantErr: true},
		{in: "5", wantErr: true},
		{in: "mon", wantDue: "2026-10-12 09:00"},
		{in: "daily", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
		{in: "watch the sun", wantErr: true},
		{in: "", wantErr: true},
		{in: "خرید ۲ نان", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			cal := tt.cal
			if cal == "" {
				cal = greg
			}
			due, rule, err := Parser{}.ParseWhen(tt.in, now, cal)
			if tt.wantErr {
				if !errors.Is(err, ErrNoDate) {
					t.Fatalf("err = %v, want ErrNoDate", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := due.Format(layout); s != tt.wantDue {
				t.Errorf("due = %s, want %s", s, tt.wantDue)
			}
			if !reflect.DeepEqual(rule, tt.wantRule) {
				t.Errorf("rule = %+v, want %+v", rule, tt.wantRule)
			}
		})
	}
}

func TestWeekdayOnSameDay(t *testing.T) {
	// Saturday 2026-10-10 10:00: "saturday 9am" has passed, so next week;
	// "next saturday" is never today.
	now := parseNow(t, "2026-10-10 10:00", "")
	for in, want := range map[string]string{
		"x saturday 9am":   "2026-10-17 09:00",
		"x saturday 11:00": "2026-10-10 11:00",
		"x next saturday":  "2026-10-17 09:00",
	} {
		got, _ := Parser{}.Parse(in, now, greg)
		if got.Due == nil || got.Due.Format(layout) != want {
			t.Errorf("%q: due = %v, want %s", in, got.Due, want)
		}
	}
}

func TestInElapsedKeepsSeconds(t *testing.T) {
	now := parseNow(t, "", "").Add(45 * time.Second)
	got, _ := Parser{}.Parse("x in 10m", now, greg)
	if got.Due == nil || !got.Due.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("due = %v", got.Due)
	}
}

func TestLongInputIsFast(t *testing.T) {
	for _, unit := range []string{"at ", "on ", "every ", "5 "} {
		in := strings.Repeat(unit, 4096/len(unit))
		start := time.Now()
		got, err := Parser{}.Parse(in, parseNow(t, "", ""), greg)
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Errorf("%q: took %v", unit, d)
		}
		_ = got
	}
}
