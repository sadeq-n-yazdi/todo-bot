package web

import (
	"container/list"
	"sync"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock"
)

// maxLimiterKeys is the number of keys one limiter keeps (§11.3). Above it,
// the key that was used least recently goes first.
const maxLimiterKeys = 10000

// limiter is a set of token buckets, one for each key (an IP or a user ID).
// Each bucket holds at most n tokens and gets n tokens back in each period
// per, at a steady rate. The state is in memory, on this node only: it only
// slows down abuse and coordinates nothing (docs/WEB-DESIGN.md §10).
type limiter struct {
	clk     clock.Clock
	n       float64
	per     time.Duration
	maxKeys int

	mu   sync.Mutex
	keys map[string]*list.Element // of *bucket
	lru  *list.List               // front = used last
}

type bucket struct {
	key    string
	tokens float64
	at     time.Time // when tokens was right
}

// newLimiter returns a limiter of n requests per period per key.
func newLimiter(clk clock.Clock, n int, per time.Duration) *limiter {
	return &limiter{clk: clk, n: float64(n), per: per, maxKeys: maxLimiterKeys,
		keys: map[string]*list.Element{}, lru: list.New()}
}

// refill returns the bucket of key with the tokens of now. It creates a full
// bucket for a new key, and removes the oldest key above maxKeys.
func (l *limiter) refill(key string, now time.Time) *bucket {
	if e, ok := l.keys[key]; ok {
		l.lru.MoveToFront(e)
		b := e.Value.(*bucket)
		if d := now.Sub(b.at); d > 0 {
			b.tokens = min(l.n, b.tokens+l.n*float64(d)/float64(l.per))
		}
		b.at = now
		return b
	}
	b := &bucket{key: key, tokens: l.n, at: now}
	l.keys[key] = l.lru.PushFront(b)
	for l.lru.Len() > l.maxKeys {
		old := l.lru.Back()
		l.lru.Remove(old)
		delete(l.keys, old.Value.(*bucket).key)
	}
	return b
}

// allow takes one token of key. If the bucket is empty, it takes nothing
// and returns false with the time until the next token.
func (l *limiter) allow(key string) (ok bool, retry time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key, l.clk.Now())
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(l.per) / l.n)
}

// Limits of §11.3. The redeem failure limits belong to auth_code.go.
const (
	readyzPerMin  = 60  // GET /readyz, per IP
	miniAppPerMin = 10  // POST /api/auth/miniapp, per IP
	codePer10Min  = 5   // POST /api/auth/code, per user
	redeemPerMin  = 10  // POST /api/auth/code/redeem, per IP
	widgetPerMin  = 10  // GET /auth/telegram, per IP
	userPerMin    = 300 // every other /api/* route with a session, per user
)

// limits holds the limiters of the routes.
type limits struct {
	readyz, miniApp, redeem, widget *limiter // per IP
	code, user                      *limiter // per user
}

func newLimits(clk clock.Clock) limits {
	return limits{
		readyz:  newLimiter(clk, readyzPerMin, time.Minute),
		miniApp: newLimiter(clk, miniAppPerMin, time.Minute),
		redeem:  newLimiter(clk, redeemPerMin, time.Minute),
		widget:  newLimiter(clk, widgetPerMin, time.Minute),
		code:    newLimiter(clk, codePer10Min, 10*time.Minute),
		user:    newLimiter(clk, userPerMin, time.Minute),
	}
}
