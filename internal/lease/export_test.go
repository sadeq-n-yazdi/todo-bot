package lease

import (
	"context"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// RunWithIdle is Run with a hook that reports, before each blocking wait,
// the time before which nothing is due.
func RunWithIdle(ctx context.Context, st store.Store, clk clock.Clock, name, node string, ttl time.Duration,
	lead func(context.Context, int64), idle func(time.Time)) error {
	l := newLoop(st, clk, name, node, ttl, lead)
	l.idle = idle
	return l.run(ctx)
}
