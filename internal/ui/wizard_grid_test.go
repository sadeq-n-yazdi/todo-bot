package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func gdate(y, m, d int) calendar.Date { return calendar.Date{Year: y, Month: m, Day: d} }

func countDays(rows [][]DayCell) (n int) {
	for _, r := range rows {
		for _, c := range r {
			if c.Day > 0 {
				n++
			}
		}
	}
	return n
}

func TestMonthGridShape(t *testing.T) {
	today := gdate(2026, 10, 13)
	cases := []struct {
		name        string
		cal         calendar.Kind
		y, m        int
		days        int
		firstCol    int // column of day 1
		first, last calendar.Date
	}{
		// 1 Oct 2026 is a Thursday: Monday-first column 3.
		{"gregorian oct 2026", calendar.Gregorian, 2026, 10, 31, 3, gdate(2026, 10, 1), gdate(2026, 10, 31)},
		// 1 Mehr 1405 = 23 Sep 2026, a Wednesday: Saturday-first column 4.
		{"jalali mehr 1405", calendar.Jalali, 1405, 7, 30, 4, gdate(2026, 9, 23), gdate(2026, 10, 22)},
		// Esfand 1403 has 30 days (leap year): 1 Esfand = 19 Feb 2025.
		{"jalali esfand 1403", calendar.Jalali, 1403, 12, 30, 4, gdate(2025, 2, 19), gdate(2025, 3, 20)},
		// Esfand 1404 has 29 days: 1 Esfand = 20 Feb 2026.
		{"jalali esfand 1404", calendar.Jalali, 1404, 12, 29, 6, gdate(2026, 2, 20), gdate(2026, 3, 20)},
		{"gregorian feb 2028 leap", calendar.Gregorian, 2028, 2, 29, 1, gdate(2028, 2, 1), gdate(2028, 2, 29)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := MonthGrid(c.cal, c.y, c.m, today)
			for i, r := range rows {
				if len(r) != 7 {
					t.Fatalf("row %d has %d cells", i, len(r))
				}
			}
			if got := countDays(rows); got != c.days {
				t.Fatalf("days = %d, want %d", got, c.days)
			}
			if rows[0][c.firstCol].Day != 1 || rows[0][c.firstCol].Date != c.first {
				t.Fatalf("cell [0][%d] = %+v, want day 1 on %v", c.firstCol, rows[0][c.firstCol], c.first)
			}
			for i := 0; i < c.firstCol; i++ {
				if rows[0][i] != (DayCell{}) {
					t.Errorf("leading padding [0][%d] = %+v", i, rows[0][i])
				}
			}
			lastRow := rows[len(rows)-1]
			var last DayCell
			for _, cell := range lastRow {
				if cell.Day > 0 {
					last = cell
				}
			}
			if last.Day != c.days || last.Date != c.last {
				t.Fatalf("last = %+v, want day %d on %v", last, c.days, c.last)
			}
			for _, r := range rows[:len(rows)-1] {
				for _, cell := range r {
					if cell.Day == 0 && r[0].Day != 0 && r[6].Day != 0 {
						t.Errorf("padding inside a full week")
					}
				}
			}
		})
	}
}

func TestMonthGridPast(t *testing.T) {
	rows := MonthGrid(calendar.Gregorian, 2026, 10, gdate(2026, 10, 13))
	for _, r := range rows {
		for _, c := range r {
			switch {
			case c.Day == 0 && c.Past:
				t.Errorf("padding is Past")
			case c.Day > 0 && c.Past != (c.Day < 13):
				t.Errorf("day %d Past = %v", c.Day, c.Past)
			}
		}
	}
	// Today itself is choosable; a Jalali month fully before today is all past.
	for _, r := range MonthGrid(calendar.Jalali, 1405, 6, gdate(2026, 10, 13)) {
		for _, c := range r {
			if c.Day > 0 && !c.Past {
				t.Errorf("shahrivar day %d not past", c.Day)
			}
		}
	}
}

func TestFirstWeekdayPerCalendar(t *testing.T) {
	if FirstWeekday(calendar.Jalali) != time.Saturday || FirstWeekday(calendar.Gregorian) != time.Monday {
		t.Fatal("first weekdays")
	}
}

func gridView(cal calendar.Kind, y, m int) WizardView {
	return WizardView{
		Draft: model.Draft{Step: model.StepDate, Version: 77},
		Grid:  &calendar.Date{Year: y, Month: m},
	}
}

func TestGridFixtures(t *testing.T) {
	for _, c := range []struct {
		name string
		v    WizardView
		lc   i18n.Locale
	}{
		{"gregorian_en", gridView(calendar.Gregorian, 2026, 10), locEnG},
		{"jalali_fa", gridView(calendar.Jalali, 1405, 7), locFaJ},
		{"jalali_esfand_1403_en", gridView(calendar.Jalali, 1403, 12), locEnJ},
	} {
		t.Run(c.name, func(t *testing.T) {
			golden(t, "wizard_grid_"+c.name+".golden", dump(WizardCard(c.v, c.lc, testLoc, testNow)))
		})
	}
}

func TestGridKeyboard(t *testing.T) {
	t.Run("gregorian current month has no previous", func(t *testing.T) {
		msg := WizardCard(gridView(calendar.Gregorian, 2026, 10), locEnG, testLoc, testNow)
		if !strings.Contains(msg.Text, "October 2026") {
			t.Fatalf("text %q", msg.Text)
		}
		head := msg.Keyboard[0]
		if len(head) != 7 || head[0].Text != "Mo" || head[6].Text != "Su" {
			t.Fatalf("header %+v", head)
		}
		nav := msg.Keyboard[len(msg.Keyboard)-2]
		if len(nav) != 1 || nav[0].Data != "wm:77:g202611" {
			t.Fatalf("nav %+v", nav)
		}
		var past, pickable, pad int
		for _, row := range msg.Keyboard[1 : len(msg.Keyboard)-2] {
			for _, b := range row {
				switch {
				case b.Data == "wz:77":
					pad++
				case b.Data == "wz:77:p":
					past++
				case strings.HasPrefix(b.Data, "wd:77:2026"):
					pickable++
				default:
					t.Errorf("unexpected button %+v", b)
				}
			}
		}
		if past != 12 || pickable != 19 || pad != 3+1 {
			t.Fatalf("past %d pickable %d pad %d", past, pickable, pad)
		}
	})
	t.Run("year boundary", func(t *testing.T) {
		msg := WizardCard(gridView(calendar.Gregorian, 2026, 12), locEnG, testLoc, testNow)
		nav := msg.Keyboard[len(msg.Keyboard)-2]
		if len(nav) != 2 || nav[0].Data != "wm:77:g202611" || nav[1].Data != "wm:77:g202701" {
			t.Fatalf("nav %+v", nav)
		}
		msg = WizardCard(gridView(calendar.Gregorian, 2027, 1), locEnG, testLoc, testNow)
		nav = msg.Keyboard[len(msg.Keyboard)-2]
		if nav[0].Data != "wm:77:g202612" || nav[1].Data != "wm:77:g202702" {
			t.Fatalf("nav %+v", nav)
		}
	})
	t.Run("jalali farsi digits and year boundary", func(t *testing.T) {
		// Today is 21 Mehr 1405; Esfand 1405 -> Farvardin 1406.
		msg := WizardCard(gridView(calendar.Jalali, 1405, 12), locFaJ, testLoc, testNow)
		if !strings.Contains(msg.Text, "اسفند ۱۴۰۵") {
			t.Fatalf("text %q", msg.Text)
		}
		head := msg.Keyboard[0]
		if head[0].Text != "ش" || head[6].Text != "ج" {
			t.Fatalf("header %+v", head)
		}
		if msg.Keyboard[1][0].Text == "" {
			t.Fatal("empty button text")
		}
		nav := msg.Keyboard[len(msg.Keyboard)-2]
		if len(nav) != 2 || nav[0].Data != "wm:77:j140511" || nav[1].Data != "wm:77:j140601" {
			t.Fatalf("nav %+v", nav)
		}
		var sawDigits bool
		for _, row := range msg.Keyboard[1 : len(msg.Keyboard)-2] {
			for _, b := range row {
				if b.Text == "۱۵" {
					sawDigits = true
				}
				if strings.ContainsAny(b.Text, "0123456789") {
					t.Errorf("ASCII digit in %q", b.Text)
				}
			}
		}
		if !sawDigits {
			t.Error("no Persian digits")
		}
	})
	t.Run("jalali current month", func(t *testing.T) {
		msg := WizardCard(gridView(calendar.Jalali, 1405, 7), locEnJ, testLoc, testNow)
		if !strings.Contains(msg.Text, "Mehr 1405") {
			t.Fatalf("text %q", msg.Text)
		}
		nav := msg.Keyboard[len(msg.Keyboard)-2]
		if len(nav) != 1 || nav[0].Data != "wm:77:j140508" {
			t.Fatalf("nav %+v", nav)
		}
		// day 22 Mehr 1405 = 14 Oct 2026 is choosable; 20 Mehr is past.
		var found22, past20 bool
		for _, row := range msg.Keyboard[1 : len(msg.Keyboard)-2] {
			for _, b := range row {
				if b.Text == "22" && b.Data == "wd:77:20261014" {
					found22 = true
				}
				if b.Text == "20" && b.Data == "wz:77:p" {
					past20 = true
				}
			}
		}
		if !found22 || !past20 {
			t.Fatalf("found22 %v past20 %v", found22, past20)
		}
	})
	t.Run("last rows are quick picks and cancel", func(t *testing.T) {
		msg := WizardCard(gridView(calendar.Gregorian, 2026, 10), locEnG, testLoc, testNow)
		last := msg.Keyboard[len(msg.Keyboard)-1]
		if len(last) != 2 || last[0].Data != "wd:77:q" || last[1].Data != "wn:77:x" {
			t.Fatalf("last row %+v", last)
		}
	})
}
