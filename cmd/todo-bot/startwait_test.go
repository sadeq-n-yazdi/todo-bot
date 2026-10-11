package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMaxStartWaitDefault(t *testing.T) {
	if maxStartWait != 5*time.Minute {
		t.Errorf("maxStartWait = %v, want 5m", maxStartWait)
	}
}

// A long RetryAfter must not hold the startup for its full length, and the
// log line must show the wait actually used.
func TestStartupCapsRetryAfterAndLogsWait(t *testing.T) {
	mock := newMockTelegram(t)
	mock.getMeCode, mock.retryAfter = http.StatusTooManyRequests, 3600 // one hour
	setTelegram(t, mock.srv.URL)
	oldMax := maxStartWait
	maxStartWait = 50 * time.Millisecond
	t.Cleanup(func() { maxStartWait = oldMax })

	var logs syncBuf
	log := slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = connectTelegram(ctx, "tokC", log)
	}()
	waitUntil(t, 10*time.Second, "second getMe despite a 1h RetryAfter", func() bool {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		n := 0
		for _, c := range mock.calls {
			if c.token == "tokC" && c.method == "getMe" {
				n++
			}
		}
		return n >= 2
	})
	cancel()
	<-done
	if !strings.Contains(logs.String(), "in=50ms") {
		t.Errorf("log lacks the capped wait in=50ms: %s", logs.String())
	}
}
