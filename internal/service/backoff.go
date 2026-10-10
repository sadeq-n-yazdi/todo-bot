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
func retryDelay(attempts int) (delay time.Duration, giveUp bool) {
	// TODO(human): implement the retry policy.
	return 0, true
}
