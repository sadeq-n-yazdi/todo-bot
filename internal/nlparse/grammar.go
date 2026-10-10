package nlparse

import (
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

type dateKind int

const (
	dateOffset  dateKind = iota + 1 // today / tomorrow / day after tomorrow
	dateWeekday                     // monday, next monday
	dateYMD                         // an explicit calendar date, year optional
)

type dateSpec struct {
	kind    dateKind
	offset  int          // dateOffset: days from today
	wd      time.Weekday // dateWeekday
	next    bool         // dateWeekday: "next monday" is never today
	cal     calendar.Kind
	y, m, d int
	hasYear bool
}

type timeSpec struct{ h, m int }

type relSpec struct {
	n int
	u unit
}

type recSpec struct {
	freq     recur.Freq
	interval int
	wds      []time.Weekday
	monthDay int // 0 = day of month of "now"
}

// phrase is the structured meaning of a matched date phrase.
type phrase struct {
	date    *dateSpec
	tm      *timeSpec
	rel     *relSpec
	rec     *recSpec
	tonight bool
}

type parser struct {
	toks []token
	i    int
	cal  calendar.Kind
	ph   phrase
}

func (p *parser) key(k int) string {
	if k < 0 || k >= len(p.toks) {
		return ""
	}
	return p.toks[k].key
}

// parseFrom reports whether toks[start:] is, in full, one date phrase.
func parseFrom(toks []token, start int, cal calendar.Kind) (phrase, bool) {
	p := &parser{toks: toks, i: start, cal: cal}
	for p.i < len(toks) {
		switch {
		case p.i == start && p.matchRecurrence():
		case p.matchRel(), p.matchTime(), p.matchDate():
		case isConnector(p.key(p.i)) && p.i+1 < len(toks):
			p.i++
		default:
			return phrase{}, false
		}
	}
	ph := p.ph
	if ph.date == nil && ph.tm == nil && ph.rel == nil && ph.rec == nil {
		return phrase{}, false
	}
	if ph.rel != nil && (ph.rel.u == unitMinute || ph.rel.u == unitHour) && (ph.date != nil || ph.tm != nil) {
		return phrase{}, false
	}
	return ph, true
}

func isConnector(k string) bool { return k == "at" || k == "on" || k == "in" || k == "@" }

func isAtWord(k string) bool { return k == "at" || k == "@" || k == "ساعت" }

// ---- time of day ----

var timeRe = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?(am|pm)?$`)

func (p *parser) matchTime() bool {
	if p.ph.tm != nil {
		return false
	}
	j := p.i
	conn := false
	if isAtWord(p.key(j)) {
		conn = true
		j++
	}
	m := timeRe.FindStringSubmatch(p.key(j))
	if m == nil {
		return false
	}
	h, _ := strconv.Atoi(m[1])
	mi := 0
	if m[2] != "" {
		mi, _ = strconv.Atoi(m[2])
	}
	suffix := m[3]
	j++
	period := 0
	if suffix == "" {
		switch nk := p.key(j); {
		case nk == "am" || nk == "pm":
			suffix = nk
			j++
		case periodWords[nk] != 0:
			period = periodWords[nk]
			j++
		}
	}
	if suffix == "" && period == 0 && m[2] == "" && !conn {
		return false // a bare number is not a time
	}
	if mi > 59 {
		return false
	}
	switch {
	case suffix != "":
		if h < 1 || h > 12 {
			return false
		}
		h %= 12
		if suffix == "pm" {
			h += 12
		}
	case period != 0:
		if h < 1 || h > 12 {
			return false
		}
		h = applyPeriod(h, period)
	default:
		if h > 23 {
			return false
		}
	}
	p.ph.tm = &timeSpec{h, mi}
	p.i = j
	return true
}

func applyPeriod(h, period int) int {
	switch period {
	case periodMorning:
		return h % 12
	case periodNoon:
		if h < 7 {
			return h + 12
		}
		return h
	case periodEvening:
		if h < 12 {
			return h + 12
		}
	case periodNight:
		if h == 12 {
			return 0
		}
		if h >= 6 {
			return h + 12
		}
	}
	return h
}

// ---- relative offsets ("in 2h", "۲ ساعت دیگر") ----

var relAttachedRe = regexp.MustCompile(`^(\d{1,4})([a-z]+)$`)

func (p *parser) matchRel() bool {
	if p.ph.date != nil || p.ph.rel != nil || p.ph.rec != nil {
		return false
	}
	j := p.i
	if p.key(j) == "in" {
		j++
		if m := relAttachedRe.FindStringSubmatch(p.key(j)); m != nil {
			if u, ok := unitWords[m[2]]; ok {
				n, _ := strconv.Atoi(m[1])
				return p.setRel(n, u, j+1)
			}
		}
		if n, ok := parsePlainNum(p.key(j)); ok {
			if u, ok := unitWords[p.key(j+1)]; ok && p.key(j+1) != "ساعت" {
				return p.setRel(n, u, j+2)
			}
		}
		return false
	}
	if n, ok := parsePlainNum(p.key(j)); ok {
		if u, ok := unitWords[p.key(j+1)]; ok && (p.key(j+2) == "دیگر" || p.key(j+2) == "بعد") {
			return p.setRel(n, u, j+3)
		}
	}
	return false
}

func (p *parser) setRel(n int, u unit, next int) bool {
	if n < 1 {
		return false
	}
	p.ph.rel = &relSpec{n, u}
	p.i = next
	return true
}

// ---- dates ----

// wdAt matches a weekday name at token j, returning the tokens consumed.
func (p *parser) wdAt(j int) (time.Weekday, int, bool) {
	k := p.key(j)
	if weekdayPrefix[k] {
		if wd, ok := weekdayWords[k+p.key(j+1)]; ok && p.key(j+1) != "" {
			return wd, 2, true
		}
	}
	if wd, ok := weekdayWords[k]; ok {
		return wd, 1, true
	}
	return 0, 0, false
}

func (p *parser) matchDate() bool {
	if p.ph.date != nil || p.ph.rec != nil || p.ph.rel != nil {
		return false
	}
	k := p.key(p.i)
	switch k {
	case "today", "امروز":
		return p.setOffset(0, 1)
	case "tonight", "امشب":
		p.ph.tonight = true
		return p.setOffset(0, 1)
	case "tomorrow", "فردا":
		return p.setOffset(1, 1)
	case "پسفردا":
		return p.setOffset(2, 1)
	case "پس":
		if p.key(p.i+1) == "فردا" {
			return p.setOffset(2, 2)
		}
	case "day":
		if p.key(p.i+1) == "after" && p.key(p.i+2) == "tomorrow" {
			return p.setOffset(2, 3)
		}
	case "next", "this":
		if wd, n, ok := p.wdAt(p.i + 1); ok {
			p.ph.date = &dateSpec{kind: dateWeekday, wd: wd, next: k == "next"}
			p.i += 1 + n
			return true
		}
		return false
	}
	if wd, n, ok := p.wdAt(p.i); ok {
		next := false
		if nk := p.key(p.i + n); nk == "آینده" || nk == "بعد" || nk == "بعدی" {
			next = true
			n++
		}
		p.ph.date = &dateSpec{kind: dateWeekday, wd: wd, next: next}
		p.i += n
		return true
	}
	if ds, ok := p.numericDate(k); ok {
		p.ph.date = ds
		p.i++
		return true
	}
	return p.matchMonthName()
}

func (p *parser) setOffset(days, n int) bool {
	p.ph.date = &dateSpec{kind: dateOffset, offset: days}
	p.i += n
	return true
}

var (
	dmyRe = regexp.MustCompile(`^(\d{1,2})([/\-.])(\d{1,2})(?:[/\-.](\d{4}|\d{2}))?$`)
	ymdRe = regexp.MustCompile(`^(\d{4})([/\-.])(\d{1,2})[/\-.](\d{1,2})$`)
)

// calForYear picks the calendar a written year belongs to: years from 1700 on
// can only be Gregorian and 1200..1699 only Jalali, whatever the user's
// default calendar is. Anything else follows def.
func calForYear(y int, def calendar.Kind) calendar.Kind {
	switch {
	case y >= 1700:
		return calendar.Gregorian
	case y >= 1200:
		return calendar.Jalali
	}
	return def
}

func maxDay(cal calendar.Kind, m int) int {
	switch {
	case m == 2 && cal == calendar.Gregorian:
		return 29
	case m == 4 || m == 6 || m == 9 || m == 11:
		if cal == calendar.Gregorian {
			return 30
		}
	}
	if cal == calendar.Jalali && m >= 7 {
		return 30
	}
	return 31
}

func validYMD(cal calendar.Kind, y, m, d int, hasYear bool) bool {
	if m < 1 || m > 12 || d < 1 || d > maxDay(cal, m) {
		return false
	}
	return !hasYear || d <= cal.MonthLength(y, m)
}

func (p *parser) numericDate(k string) (*dateSpec, bool) {
	if m := ymdRe.FindStringSubmatch(k); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[3])
		d, _ := strconv.Atoi(m[4])
		cal := calForYear(y, p.cal)
		if !validYMD(cal, y, mo, d, true) {
			return nil, false
		}
		return &dateSpec{kind: dateYMD, cal: cal, y: y, m: mo, d: d, hasYear: true}, true
	}
	m := dmyRe.FindStringSubmatch(k)
	if m == nil {
		return nil, false
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[3])
	cal := p.cal
	if m[4] == "" {
		if m[2] != "/" || !validYMD(cal, 0, mo, d, false) {
			return nil, false
		}
		return &dateSpec{kind: dateYMD, cal: cal, m: mo, d: d}, true
	}
	y, _ := strconv.Atoi(m[4])
	if len(m[4]) == 2 {
		if cal == calendar.Jalali {
			y += 1400
		} else {
			y += 2000
		}
	} else {
		cal = calForYear(y, cal)
	}
	if !validYMD(cal, y, mo, d, true) {
		return nil, false
	}
	return &dateSpec{kind: dateYMD, cal: cal, y: y, m: mo, d: d, hasYear: true}, true
}

// yearAt matches a four-digit year at token j that fits calendar cal.
func (p *parser) yearAt(j int, cal calendar.Kind) (int, bool) {
	k := p.key(j)
	if len(k) != 4 {
		return 0, false
	}
	y, ok := parsePlainNum(k)
	if !ok || calForYear(y, cal) != cal || y < 1200 || y > 3000 {
		return 0, false
	}
	return y, true
}

func (p *parser) matchMonthName() bool {
	i := p.i
	// "15 Oct [2026]", "۱۵ مهر ۱۴۰۵", "15th of Oct"
	if d, ok := parseNum(p.key(i)); ok && d >= 1 && d <= 31 {
		j := i + 1
		if p.key(j) == "of" {
			j++
		}
		if mr, ok := monthWords[p.key(j)]; ok {
			return p.setMonthDate(mr, d, j+1)
		}
	}
	// "Oct 15 [2026]"
	if mr, ok := monthWords[p.key(i)]; ok {
		if d, ok := parseNum(p.key(i + 1)); ok && d >= 1 && d <= 31 {
			return p.setMonthDate(mr, d, i+2)
		}
	}
	return false
}

func (p *parser) setMonthDate(mr monthRef, d, next int) bool {
	ds := &dateSpec{kind: dateYMD, cal: mr.cal, m: mr.month, d: d}
	if y, ok := p.yearAt(next, mr.cal); ok {
		ds.y, ds.hasYear = y, true
		next++
	}
	if !validYMD(ds.cal, ds.y, ds.m, ds.d, ds.hasYear) {
		return false
	}
	p.ph.date = ds
	p.i = next
	return true
}

// ---- recurrence ----

var (
	weekdaysMonFri = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	workdaysFa     = []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Saturday}
)

// wdList matches "mon", "mon and wed", "دوشنبه و چهارشنبه", "mon, wed".
func (p *parser) wdList(j int) ([]time.Weekday, int) {
	var out []time.Weekday
	end := j
	for {
		at := end
		if k := p.key(at); k == "and" || k == "&" || k == "و" {
			at++
		}
		wd, n, ok := p.wdAt(at)
		if !ok {
			break
		}
		out = append(out, wd)
		end = at + n
	}
	if len(out) == 0 {
		return nil, j
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	uniq := out[:1]
	for _, w := range out[1:] {
		if w != uniq[len(uniq)-1] {
			uniq = append(uniq, w)
		}
	}
	return uniq, end
}

func (p *parser) setRec(r recSpec, next int) bool {
	p.ph.rec = &r
	p.i = next
	return true
}

func (p *parser) matchRecurrence() bool {
	j := p.i
	switch p.key(j) {
	case "daily", "everyday", "روزانه":
		return p.setRec(recSpec{freq: recur.Daily}, j+1)
	case "weekdays", "weekday":
		return p.setRec(recSpec{freq: recur.Weekly, wds: weekdaysMonFri}, j+1)
	case "weekly", "هفتگی":
		return p.weeklyTail(j + 1)
	case "monthly", "ماهانه":
		return p.monthlyTail(j + 1)
	case "روزهای":
		if p.key(j+1) == "کاری" {
			return p.setRec(recSpec{freq: recur.Weekly, wds: workdaysFa}, j+2)
		}
		return false
	case "every", "each", "هر":
	default:
		return false
	}
	j++
	switch p.key(j) {
	case "day", "روز":
		return p.setRec(recSpec{freq: recur.Daily}, j+1)
	case "weekday", "weekdays":
		return p.setRec(recSpec{freq: recur.Weekly, wds: weekdaysMonFri}, j+1)
	case "week", "هفته":
		return p.weeklyTail(j + 1)
	case "month", "ماه":
		return p.monthlyTail(j + 1)
	}
	if n, ok := parsePlainNum(p.key(j)); ok && n >= 1 {
		switch unitWords[p.key(j+1)] {
		case unitDay:
			return p.everyN(n, j+2)
		case unitWeek:
			return p.everyN(n*7, j+2)
		}
	}
	if wds, end := p.wdList(j); wds != nil {
		return p.setRec(recSpec{freq: recur.Weekly, wds: wds}, end)
	}
	return false
}

func (p *parser) everyN(n, next int) bool {
	if n == 1 {
		return p.setRec(recSpec{freq: recur.Daily}, next)
	}
	return p.setRec(recSpec{freq: recur.EveryN, interval: n}, next)
}

func (p *parser) weeklyTail(j int) bool {
	start := j
	if p.key(j) == "on" {
		j++
	}
	wds, end := p.wdList(j)
	if wds == nil {
		return p.setRec(recSpec{freq: recur.Weekly}, start)
	}
	return p.setRec(recSpec{freq: recur.Weekly, wds: wds}, end)
}

func (p *parser) monthlyTail(j int) bool {
	start := j
	if p.key(j) == "on" {
		j++
	}
	if p.key(j) == "the" {
		j++
	}
	if k := p.key(j); k == "day" || k == "روز" {
		j++
	}
	if d, ok := parseNum(p.key(j)); ok && d >= 1 && d <= 31 && !p.timeWordAt(j+1) {
		return p.setRec(recSpec{freq: recur.Monthly, monthDay: d}, j+1)
	}
	return p.setRec(recSpec{freq: recur.Monthly}, start)
}

// timeWordAt reports whether token j is an am/pm or Farsi day-period word,
// meaning the number before it is an hour, not a day of month.
func (p *parser) timeWordAt(j int) bool {
	k := p.key(j)
	return k == "am" || k == "pm" || periodWords[k] != 0
}
