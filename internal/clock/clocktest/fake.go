// Package clocktest provides a manually driven clock for tests.
package clocktest

import (
	"sync"
	"time"
)

// Fake is a Clock whose time only moves when Set or Advance is called.
// It is safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

// New returns a Fake set to t.
func New(t time.Time) *Fake { return &Fake{now: t} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After returns a channel that receives once the fake time reaches now+d.
// A non-positive d fires immediately.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := f.now.Add(d)
	if d <= 0 {
		ch <- f.now
		return ch
	}
	f.waiters = append(f.waiters, waiter{at: at, ch: ch})
	return ch
}

// Advance moves time forward by d and fires due After channels.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	t := f.now.Add(d)
	f.mu.Unlock()
	f.Set(t)
}

// Set moves time to t and fires due After channels.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
	kept := f.waiters[:0]
	for _, w := range f.waiters {
		if !w.at.After(t) {
			w.ch <- t
		} else {
			kept = append(kept, w)
		}
	}
	f.waiters = kept
}

// Waiters reports how many After channels are pending; tests use it to
// wait until a goroutine is blocked on the clock.
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiters)
}
