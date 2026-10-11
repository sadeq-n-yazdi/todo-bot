package service

import "time"

// retryDelay decides what happens after a transient send failure of a
// reminder (network error, Telegram 5xx, or a 429 too long to wait out
// inline). attempts is the number of failed sends so far, including this
// one (1 on the first failure).
//
// It returns how long to wait before the next try, or giveUp=true to mark
// the reminder failed for good. ReminderFailed (#8) stores now+delay as the
// new fire_at and releases the claim, so any node may retry it.
var backOffDelays = []time.Duration{
	5 * time.Second,
	// 10 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	// 5 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	// 1 * time.Hour,
	// 2 * time.Hour,
	// 4 * time.Hour,
	// 8 * time.Hour,
	// 12 * time.Hour,
	// 24 * time.Hour,
}

func retryDelay(attempts int) (delay time.Duration, giveUp bool) {
	if attempts > 0 && attempts <= len(backOffDelays) {
		return backOffDelays[attempts-1], false
	}
	return 0, true
}
