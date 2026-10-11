package ui

import "testing"

func TestSettingsEncodeDecode(t *testing.T) {
	cases := []struct {
		a    Action
		want string
	}{
		{Action{Kind: ActSetLang, Arg: "en"}, "sl:0:en"},
		{Action{Kind: ActSetLang, Arg: "fa"}, "sl:0:fa"},
		{Action{Kind: ActSetCal, Arg: "gregorian"}, "sc:0:gregorian"},
		{Action{Kind: ActSetCal, Arg: "jalali"}, "sc:0:jalali"},
	}
	for _, c := range cases {
		got := Encode(c.a)
		if got != c.want || len(got) > 64 {
			t.Errorf("Encode(%+v) = %q, want %q", c.a, got, c.want)
		}
		back, err := Decode(got)
		if err != nil || back != c.a {
			t.Errorf("Decode(%q) = %+v, %v; want %+v", got, back, err, c.a)
		}
	}
}

func TestSettingsDecodeRejects(t *testing.T) {
	for _, s := range []string{
		"sl:0", "sl:0:", "sl:0:xx", "sl:0:jalali", "sl:5:en", "sl:0:en:x",
		"sc:0", "sc:0:mayan", "sc:0:en", "sc:1:jalali",
		"sl:fa", "sc:jalali", // old format: ui.Decode does not accept it
	} {
		if a, err := Decode(s); err == nil {
			t.Errorf("Decode(%q) = %+v, want error", s, a)
		}
	}
}

func TestSettingsEncodeDoesNotRepair(t *testing.T) {
	if got := Encode(Action{Kind: ActSetLang, ID: 7, Arg: "fa"}); got != "sl:0:fa" {
		t.Errorf("ID not cleared: %q", got)
	}
	if _, err := Decode(Encode(Action{Kind: ActSetLang, Arg: "xx"})); err == nil {
		t.Error("invalid arg must not round-trip")
	}
	if _, err := Decode(Encode(Action{Kind: ActSetCal})); err == nil {
		t.Error("missing arg must not round-trip")
	}
}
