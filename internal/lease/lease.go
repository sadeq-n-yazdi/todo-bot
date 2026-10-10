// Package lease runs the leader-lease loop that decides which node does a
// singleton job, such as polling Telegram (docs/ARCHITECTURE.md section 9).
package lease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// releaseTimeout bounds the final ReleaseLease call on shutdown.
const releaseTimeout = 5 * time.Second

// Run competes for the named lease until ctx is cancelled.
//
// The holder identity is node+":"+random, generated once per Run, so two
// processes that share a node name never both win. The lease is taken or
// renewed every ttl/3. When a win carries an epoch different from the one last
// seen, lead is started in its own goroutine with a context that is cancelled
// when the lease is lost, when the node fences itself (no successful renewal
// for ttl/2, for example because the store is failing) or on shutdown. Run
// waits for lead to return before it reacquires or releases the lease. If lead
// returns by itself, it is started again at the next renewal.
//
// On shutdown the lease is released with a context that is not the cancelled
// one, so another node can take over at once. Run then returns nil.
func Run(ctx context.Context, st store.Store, clk clock.Clock, name, node string, ttl time.Duration,
	lead func(ctx context.Context, epoch int64)) error {
	l := &loop{st: st, clk: clk, name: name, holder: node + ":" + randomSuffix(), ttl: ttl, lead: lead}
	return l.run(ctx)
}

type loop struct {
	st     store.Store
	clk    clock.Clock
	name   string
	holder string
	ttl    time.Duration
	lead   func(ctx context.Context, epoch int64)

	leading bool
	epoch   int64
	lastOK  time.Time // time of the last successful renewal
	cancel  context.CancelFunc
	done    chan struct{}
}

func (l *loop) run(ctx context.Context) error {
	for {
		l.step(ctx)
		select {
		case <-ctx.Done():
			l.stopLead()
			return l.release(ctx)
		case <-l.clk.After(l.ttl / 3):
		}
	}
}

func (l *loop) step(ctx context.Context) {
	if l.leading {
		select {
		case <-l.done: // lead returned by itself
			l.leading = false
		default:
		}
	}
	now := l.clk.Now()
	got, err := l.st.AcquireLease(ctx, l.name, l.holder, now, l.ttl)
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		slog.Warn("lease: acquire failed", "lease", l.name, "err", err)
		if l.leading && now.Sub(l.lastOK) >= l.ttl/2 {
			slog.Warn("lease: fencing, no renewal for ttl/2", "lease", l.name)
			l.stopLead()
		}
	case got.Holder != l.holder:
		if l.leading {
			slog.Info("lease: lost", "lease", l.name, "holder", got.Holder)
			l.stopLead()
		}
	default:
		if l.leading && now.Sub(l.lastOK) >= l.ttl/2 {
			// The renewal worked, but we were past the fence point first.
			l.stopLead()
		}
		l.lastOK = now
		if l.leading && got.Epoch == l.epoch {
			return
		}
		l.stopLead() // a new epoch: the previous lead is stale
		l.startLead(ctx, got.Epoch)
	}
}

func (l *loop) startLead(ctx context.Context, epoch int64) {
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.leading, l.epoch, l.cancel, l.done = true, epoch, cancel, done
	go func() {
		defer close(done)
		l.lead(lctx, epoch)
	}()
}

// stopLead cancels the lead context and waits for lead to return.
func (l *loop) stopLead() {
	if !l.leading {
		return
	}
	l.cancel()
	<-l.done
	l.leading = false
}

func (l *loop) release(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	return l.st.ReleaseLease(rctx, l.name, l.holder)
}

func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("lease: crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
