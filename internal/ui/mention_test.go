package ui

import "testing"

func TestMention(t *testing.T) {
	for in, want := range map[string]string{
		"":       "",
		"wife":   "@wife",
		"a<b>":   "@a&lt;b&gt;",
		"Sara_1": "@Sara_1",
	} {
		if got := Mention(in); got != want {
			t.Errorf("Mention(%q) = %q, want %q", in, got, want)
		}
	}
}
