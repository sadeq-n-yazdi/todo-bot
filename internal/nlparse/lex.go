package nlparse

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// token is one whitespace-separated word of the input.
type token struct {
	key        string // lower-cased, ZWNJ removed, trailing punctuation trimmed
	start, end int    // rune offsets into the original input
}

var keyReplacer = strings.NewReplacer("\u200c", "", "\u200f", "", "\u200e", "", "ي", "ی", "ك", "ک")

func keyOf(s string) string {
	s = strings.ToLower(keyReplacer.Replace(s))
	return strings.TrimRight(s, ".,;:!?،؛")
}

// tokenize splits normalised on whitespace, recording rune offsets. The
// normalised and original strings have the same rune count.
func tokenize(normalised string) []token {
	runes := []rune(normalised)
	var toks []token
	for i := 0; i < len(runes); {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && !unicode.IsSpace(runes[j]) {
			j++
		}
		if k := keyOf(string(runes[i:j])); k != "" {
			toks = append(toks, token{key: k, start: i, end: j})
		}
		i = j
	}
	return toks
}

var numRe = regexp.MustCompile(`^(\d{1,4})(st|nd|rd|th|مین|ام|م)?$`)

// parseNum parses "15", "1st", "31st", "1ام".
func parseNum(key string) (int, bool) {
	m := numRe.FindStringSubmatch(key)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// parsePlainNum parses 1..4 digits and nothing else.
func parsePlainNum(key string) (int, bool) {
	if key == "" || len(key) > 4 {
		return 0, false
	}
	n, err := strconv.Atoi(key)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
