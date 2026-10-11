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
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// releaseTimeout bounds the final ReleaseLease call on shutdown.
const releaseTimeout = 5 * time.Second

// Run competes for the named lease until ctx is cancelled.
//
// The holder identity is node+":"+random, generated once per Run, so two
// processes that share a node name never both win. The lease is taken or
// renewed every ttl/3, measured from the start of each attempt.
//
// When a win carries an epoch different from the one last seen, lead is
// started in its own goroutine with a context that is cancelled when the
// lease is lost, when the node fences itself and on shutdown. The fence is
// driven by clk: lead's context is cancelled at lastRenewalStart+ttl/2 even
// while a store call is still in flight or ignores its context, and a renewal
// that only returns after that point counts as failed.
//
// lead must return promptly once its context is cancelled; Run waits for it
// before it reacquires or releases the lease. lead may be called again with
// the same epoch, after a fence that was followed by a recovery, or when a
// previous call returned by itself (it is then restarted at the next renewal).
// Per-epoch setup in lead must therefore be idempotent. Invocations never
// overlap.
//
// On shutdown the lease is released with a context that is not the cancelled
// one, so another node can take over at once. Run returns the error of that
// release, or nil.
func Run(ctx context.Context, st store.Store, clk clock.Clock, name, node string, ttl time.Duration,
	lead func(ctx context.Context, epoch int64)) error {
	return newLoop(st, clk, name, node, ttl, lead).run(ctx)
}

func newLoop(st store.Store, clk clock.Clock, name, node string, ttl time.Duration,
	lead func(ctx context.Context, epoch int64)) *loop {
	return &loop{st: st, clk: clk, name: name, holder: node + ":" + randomSuffix(), ttl: ttl, lead: lead}
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
	lastOK  time.Time // start time of the last successful renewal
	fence   <-chan time.Time
	fenceAt time.Time
	cancel  context.CancelFunc
	done    chan struct{}

	// At most one AcquireLease is in flight per Run, so the writes of this
	// process reach the store in increasing "now" order.
	inflight       chan acquireResult
	cancelInflight context.CancelFunc

	// idle is a test hook, called before each blocking wait with the time
	// before which nothing is due. It is nil in production.
	idle func(next time.Time)
}

type acquireResult struct {
	lease model.Lease
	err   error
}

func (l *loop) run(ctx context.Context) error {
	for {
		start := l.clk.Now()
		l.attempt(ctx, start)
		if ctx.Err() != nil || !l.wait(ctx, start.Add(l.ttl/3)) {
			l.stopLead()
			return l.release(ctx)
		}
	}
}

// wait blocks until the next attempt is due, fencing if the fence fires first.
// It reports false when ctx is done.
func (l *loop) wait(ctx context.Context, until time.Time) bool {
	tick := l.clk.After(until.Sub(l.clk.Now()))
	for {
		if l.idle != nil {
			next := until
			if l.leading && l.fenceAt.Before(next) {
				next = l.fenceAt
			}
			l.idle(next)
		}
		select {
		case <-tick:
			return true
		case <-l.fence:
			slog.Warn("lease: fencing, no renewal for ttl/2", "lease", l.name)
			l.stopLead()
		case <-ctx.Done():
			return false
		}
	}
}

// attempt takes or renews the lease once. The store call runs in its own
// goroutine so that a call that ignores its context cannot delay the fence.
// If the previous call is still running, no new call is started: the attempt
// counts as failed. Its late result is discarded.
func (l *loop) attempt(ctx context.Context, start time.Time) {
	l.reapEarlyReturn()
	l.drainInflight()
	if l.inflight != nil {
		// The previous call is still running: this attempt fails, and the old
		// call's result is discarded when it arrives (see drainInflight).
		slog.Warn("lease: previous acquire still running", "lease", l.name)
		if l.leading && !l.clk.Now().Before(l.lastOK.Add(l.ttl/2)) {
			l.stopLead()
		}
		return
	}
	actx, acancel := context.WithCancel(ctx)
	resCh := make(chan acquireResult, 1)
	l.inflight, l.cancelInflight = resCh, acancel
	go func() {
		got, err := l.st.AcquireLease(actx, l.name, l.holder, start, l.ttl)
		resCh <- acquireResult{got, err}
	}()
	var bound <-chan time.Time // while not leading, a call may not stall the loop for long
	if !l.leading {
		bound = l.clk.After(l.ttl / 3)
	}
	var res acquireResult
	got, fenceFired := false, false
	select {
	case res = <-l.inflight:
		got = true
		l.clearInflight()
	case <-l.fence:
		fenceFired = true
		l.cancelInflight()
	case <-bound:
		l.cancelInflight()
	case <-ctx.Done():
		return
	}
	if ctx.Err() != nil {
		return
	}

	// A renewal that returns at or after the fence point has failed.
	if l.leading && (fenceFired || !l.clk.Now().Before(l.lastOK.Add(l.ttl/2))) {
		slog.Warn("lease: fencing, no renewal for ttl/2", "lease", l.name)
		l.stopLead()
		return
	}
	switch {
	case !got:
		slog.Warn("lease: acquire timed out", "lease", l.name)
	case res.err != nil:
		slog.Warn("lease: acquire failed", "lease", l.name, "err", res.err)
	case res.lease.Holder != l.holder:
		if l.leading {
			slog.Info("lease: lost", "lease", l.name, "holder", res.lease.Holder)
			l.stopLead()
		}
	default:
		l.lastOK = start
		if l.leading && res.lease.Epoch == l.epoch {
			l.armFence()
			return
		}
		l.stopLead() // a new epoch: the previous lead is stale
		l.startLead(ctx, res.lease.Epoch)
	}
}

// drainInflight drops the result of a call that finished since the last attempt.
func (l *loop) drainInflight() {
	if l.inflight == nil {
		return
	}
	select {
	case <-l.inflight:
		l.clearInflight()
	default:
	}
}

func (l *loop) clearInflight() {
	l.cancelInflight()
	l.inflight, l.cancelInflight = nil, nil
}

// reapEarlyReturn notices a lead that returned by itself.
func (l *loop) reapEarlyReturn() {
	if !l.leading {
		return
	}
	select {
	case <-l.done:
		slog.Warn("lease: lead returned while leading; restarting", "lease", l.name, "epoch", l.epoch)
		l.cancel()
		l.leading, l.fence = false, nil
	default:
	}
}

func (l *loop) armFence() {
	l.fenceAt = l.lastOK.Add(l.ttl / 2)
	l.fence = l.clk.After(l.fenceAt.Sub(l.clk.Now()))
}

func (l *loop) startLead(ctx context.Context, epoch int64) {
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.leading, l.epoch, l.cancel, l.done = true, epoch, cancel, done
	l.armFence()
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
	l.leading, l.fence = false, nil
}

// release gives an in-flight call a short time to finish, so that its write
// cannot land after the release and retake the lease, then releases.
//
// Late-write case: an AcquireLease that ignores its context and outlives the
// releaseTimeout wait can still land after ReleaseLease. It then retakes the
// released row with a new epoch, and no node is leading under it. The other
// nodes are locked out until that call's start time plus ttl. This costs
// availability only. It is not a safety problem, because no leader runs under
// the retaken row. A monotonic held_until in the store (issue #50) does not
// change it: the retaking write is newer than the release, which set held_until
// to 0. The wait is bounded, so shutdown does not hang on a stuck store call.
func (l *loop) release(ctx context.Context) error {
	if l.inflight != nil {
		l.cancelInflight()
		select {
		case <-l.inflight:
		case <-l.clk.After(releaseTimeout):
		}
	}
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
