package web

import (
	"strconv"
	"testing"
	"time"
)

func TestRetryAfter(t *testing.T) {
	for d, want := range map[time.Duration]int{0: 1, time.Millisecond: 1, time.Second: 1, 1500 * time.Millisecond: 2, time.Minute: 60} {
		if got := retryAfter(d); got != strconv.Itoa(want) {
			t.Errorf("retryAfter(%v) = %s, want %d", d, got, want)
		}
	}
}
