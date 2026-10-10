package nlparse

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// Words are stored in "key" form (see keyOf): lower case, no ZWNJ.

var weekdayWords = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday,
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,

	"یکشنبه": time.Sunday, "دوشنبه": time.Monday, "سهشنبه": time.Tuesday,
	"چهارشنبه": time.Wednesday, "پنجشنبه": time.Thursday, "جمعه": time.Friday,
	"شنبه": time.Saturday,
}

// weekdayPrefix are Farsi first halves of weekday names that may be written
// with a plain space instead of a ZWNJ ("سه شنبه").
var weekdayPrefix = map[string]bool{"یک": true, "دو": true, "سه": true, "چهار": true, "پنج": true}

type monthRef struct {
	cal   calendar.Kind
	month int
}

var monthWords = buildMonthWords()

func buildMonthWords() map[string]monthRef {
	m := map[string]monthRef{}
	for _, cal := range []calendar.Kind{calendar.Gregorian, calendar.Jalali} {
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			for i, name := range i18n.MonthNames(cal, lang) {
				m[keyOf(name)] = monthRef{cal, i + 1}
			}
		}
	}
	for i, abbr := range []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"} {
		m[abbr] = monthRef{calendar.Gregorian, i + 1}
	}
	m["sept"] = monthRef{calendar.Gregorian, 9}
	m["ژوییه"] = monthRef{calendar.Gregorian, 7}
	m["ژوین"] = monthRef{calendar.Gregorian, 6}
	m["می"] = monthRef{calendar.Gregorian, 5}
	m["ordibehest"] = monthRef{calendar.Jalali, 2}
	m["اذر"] = monthRef{calendar.Jalali, 9}
	m["ابان"] = monthRef{calendar.Jalali, 8}
	return m
}

// Farsi day-period words after an hour: "۹ صبح", "۵ عصر", "۸ شب".
const (
	periodMorning = iota + 1
	periodNoon
	periodEvening
	periodNight
)

var periodWords = map[string]int{
	"صبح": periodMorning, "ظهر": periodNoon, "بعدازظهر": periodNoon,
	"عصر": periodEvening, "شب": periodNight,
}

type unit int

const (
	unitMinute unit = iota + 1
	unitHour
	unitDay
	unitWeek
)

var unitWords = map[string]unit{
	"m": unitMinute, "min": unitMinute, "mins": unitMinute, "minute": unitMinute, "minutes": unitMinute,
	"h": unitHour, "hr": unitHour, "hrs": unitHour, "hour": unitHour, "hours": unitHour,
	"d": unitDay, "day": unitDay, "days": unitDay,
	"w": unitWeek, "week": unitWeek, "weeks": unitWeek,

	"دقیقه": unitMinute, "ساعت": unitHour, "روز": unitDay, "هفته": unitWeek,
}
