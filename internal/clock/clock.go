// Package clock abstracts time so the scheduler and service can be tested
// with a fake clock.
package clock

import "time"

// Clock is the source of time for the whole application.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time                         { return time.Now() }
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }
