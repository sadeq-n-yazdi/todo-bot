package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Opener opens (and migrates) a store for a parsed database URL. The URL may
// carry a password: openers must not include it in errors or logs (use
// u.Redacted()).
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
// scheme. Accepted forms:
//
//	sqlite:///var/lib/todo-bot/todo.db   absolute path (u.Path)
//	sqlite:todo.db, sqlite:data/todo.db  relative path (u.Opaque)
//	postgres://u:p@h:5432/db?sslmode=verify-full
//	mysql://u:p@h:3306/db?tls=true
//
// The sqlite provider (registered by #25) rejects a non-empty host, so
// "sqlite://rel/x.db" is an error rather than host "rel". To build a URL
// from a file path use url.URL{Scheme: "sqlite", Path: p} for an absolute p
// and url.URL{Scheme: "sqlite", Opaque: p} for a relative one (Path with a
// relative p would render as "sqlite://p", i.e. a host).
func Open(ctx context.Context, rawURL string) (Store, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// *url.Error embeds the full URL, password included; keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
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
