package service

import (
	"testing"
	"time"
)

// Test for backOff Delay

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		attempts       int
		expectedDelay  time.Duration
		expectedGiveUp bool
	}{
		{0, 0, true}, // No attempts yet
		{1, 5 * time.Second, false},
		{2, 30 * time.Second, false},
		{3, 2 * time.Minute, false},
		{4, 10 * time.Minute, false},
		{5, 30 * time.Minute, false},
		{6, 0, true},   // Exceeds the length of backOffDelays
		{100, 0, true}, // Exceeds the length of backOffDelays
	}
	for _, tc := range cases {
		delay, giveUp := retryDelay(tc.attempts)
		if delay != tc.expectedDelay || giveUp != tc.expectedGiveUp {
			t.Errorf("retryDelay(%d) = (%v, %v), want (%v, %v)", tc.attempts, delay, giveUp, tc.expectedDelay, tc.expectedGiveUp)
		}
	}

	// Test the case where attempts exceed the length of backOffDelays
	delay, giveUp := retryDelay(len(backOffDelays) + 1)
	if delay != 0 || !giveUp {
		t.Errorf("retryDelay(%d) = (%v, %v), want (0, true)", len(backOffDelays)+1, delay, giveUp)
	}
}
