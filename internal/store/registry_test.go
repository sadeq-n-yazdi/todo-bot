package store

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRegistry(t *testing.T) {
	want := errors.New("opened")
	var got *url.URL
	Register("regtest", func(_ context.Context, u *url.URL) (Store, error) { got = u; return nil, want })

	if _, err := Open(context.Background(), "REGTEST:///tmp/x.db"); !errors.Is(err, want) {
		t.Fatalf("Open = %v, want opener error", err)
	}
	if got == nil || got.Path != "/tmp/x.db" {
		t.Fatalf("opener got %v", got)
	}

	_, err := Open(context.Background(), "nosuch://h/db")
	if err == nil || !strings.Contains(err.Error(), `no provider for "nosuch"`) || !strings.Contains(err.Error(), "regtest") {
		t.Fatalf("unknown scheme error = %v", err)
	}
	if _, err := Open(context.Background(), "::bad"); err == nil {
		t.Fatal("want error for unparsable URL")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	Register("RegTest", func(context.Context, *url.URL) (Store, error) { return nil, nil })
}
