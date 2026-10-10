package store

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Opener opens (and migrates) a store for a parsed database URL.
type Opener func(ctx context.Context, u *url.URL) (Store, error)

var (
	regMu   sync.RWMutex
	openers = map[string]Opener{}
)

// Register makes a provider available under a URL scheme. It is meant to be
// called from a provider's init and panics on a duplicate or empty scheme.
func Register(scheme string, o Opener) {
	scheme = strings.ToLower(scheme)
	regMu.Lock()
	defer regMu.Unlock()
	if scheme == "" || o == nil {
		panic("store: Register with empty scheme or nil opener")
	}
	if _, dup := openers[scheme]; dup {
		panic("store: Register called twice for scheme " + scheme)
	}
	openers[scheme] = o
}

// Open parses rawURL and opens it with the provider registered for its
// scheme, e.g. "sqlite:///var/lib/todo-bot/todo.db", "sqlite:todo.db",
// "postgres://u:p@h/db?sslmode=verify-full", "mysql://u:p@h/db?tls=true".
func Open(ctx context.Context, rawURL string) (Store, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("store: bad database URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	regMu.RLock()
	o, ok := openers[scheme]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("store: no provider for %q (compiled: %s)", scheme, strings.Join(Schemes(), ", "))
	}
	return o(ctx, u)
}

// Schemes lists the registered URL schemes, sorted.
func Schemes() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(openers))
	for s := range openers {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
