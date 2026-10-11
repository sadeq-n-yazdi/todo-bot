package ui

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

// Wizard callback data (#68).

func TestWizardEncodeDecode(t *testing.T) {
	maxID := int64(math.MaxInt64)
	maxUser := strconv.FormatInt(math.MaxInt64, 10)
	cases := []struct {
		a Action
		s string
	}{
		{Action{Kind: ActWizStart}, "ws:0"},
		{Action{Kind: ActWizDate, ID: 3, Arg: "20261012"}, "wd:3:20261012"},
		{Action{Kind: ActWizDate, ID: 3, Arg: WizDateNone}, "wd:3:n"},
		{Action{Kind: ActWizDate, ID: 3, Arg: WizDateGrid}, "wd:3:o"},
		{Action{Kind: ActWizDate, ID: 3, Arg: WizDateQuick}, "wd:3:q"},
		{Action{Kind: ActWizMonth, ID: 3, Arg: "j140512"}, "wm:3:j140512"},
		{Action{Kind: ActWizMonth, ID: 3, Arg: "g202701"}, "wm:3:g202701"},
		{Action{Kind: ActWizTime, ID: 3, Arg: "1530"}, "wt:3:1530"},
		{Action{Kind: ActWizTime, ID: 3, Arg: WizTimeDefault}, "wt:3:d"},
		{Action{Kind: ActWizTime, ID: 3, Arg: WizTimeType}, "wt:3:k"},
		{Action{Kind: ActWizRepeat, ID: 3, Arg: WizRepeatYearly}, "wr:3:y"},
		{Action{Kind: ActWizAssign, ID: 3, Arg: "0"}, "wa:3:0"},
		{Action{Kind: ActWizAssign, ID: 3, Arg: "123456789"}, "wa:3:123456789"},
		{Action{Kind: ActWizNav, ID: 3, Arg: WizNavSave}, "wn:3:v"},
		{Action{Kind: ActWizNav, ID: 3, Arg: WizNavEdit}, "wn:3:e"},
		{Action{Kind: ActWizEdit, ID: 3, Arg: WizEditAssignee}, "we:3:assignee"},
		{Action{Kind: ActWizNoop}, "wz:0"},
		{Action{Kind: ActWizNoop, ID: 3, Arg: WizNoopPast}, "wz:3:p"},
		// The longest data any wizard button can carry.
		{Action{Kind: ActWizAssign, ID: maxID, Arg: maxUser}, "wa:" + maxUser + ":" + maxUser},
		{Action{Kind: ActWizEdit, ID: maxID, Arg: WizEditAssignee}, "we:" + maxUser + ":assignee"},
	}
	for _, c := range cases {
		got := Encode(c.a)
		if got != c.s {
			t.Errorf("Encode(%+v)=%q want %q", c.a, got, c.s)
		}
		if len(got) > 64 {
			t.Errorf("Encode(%+v) is %d bytes, over 64", c.a, len(got))
		}
		if d, err := Decode(c.s); err != nil || d != c.a {
			t.Errorf("Decode(%q)=%+v,%v want %+v", c.s, d, err, c.a)
		}
	}
}

func TestWizardDecodeRejects(t *testing.T) {
	for _, s := range []string{
		"ws:1", "ws:0:x",
		"wd:0:n", "wd:-1:n", "wd:3", "wd:3:", "wd:3:x", "wd:3:2026101", "wd:3:20260230", "wd:3:2026-10-12",
		"wm:3:202610", "wm:3:x202610", "wm:3:g202613", "wm:3:g202600", "wm:3:j14051", "wm:3:g000001",
		"wt:3:2400", "wt:3:1560", "wt:3:930", "wt:3:15:30", "wt:3:x",
		"wr:3:z", "wr:3",
		"wa:3:-5", "wa:3:007", "wa:3:00", "wa:3:x", "wa:3:99999999999999999999",
		"wn:3:z", "we:3:confirm", "we:3:found",
		"wz:-1", "wz:0:", "wz:0:x",
	} {
		if _, err := Decode(s); !errors.Is(err, ErrBadAction) {
			t.Errorf("Decode(%q) err=%v, want ErrBadAction", s, err)
		}
	}
}
