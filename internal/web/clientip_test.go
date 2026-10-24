package web

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name, peer, cf, xff, want string
	}{
		{"trusted with header", "127.0.0.1:1", "203.0.113.7", "", "203.0.113.7"},
		{"trusted v6 with header", "[::1]:1", "2001:db8::1", "", "2001:db8::1"},
		{"v4-mapped trusted peer", "[::ffff:127.0.0.1]:1", "203.0.113.8", "", "203.0.113.8"},
		{"trusted without header", "127.0.0.1:1", "", "", "127.0.0.1"},
		{"trusted, bad header", "127.0.0.1:1", "not-an-ip", "", "127.0.0.1"},
		{"untrusted with header", "198.51.100.1:1", "203.0.113.7", "", "198.51.100.1"},
		{"X-Forwarded-For is never read", "127.0.0.1:1", "", "203.0.113.9", "127.0.0.1"},
		{"untrusted X-Forwarded-For", "198.51.100.2:1", "", "203.0.113.9", "198.51.100.2"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.peer
		if c.cf != "" {
			r.Header.Set("CF-Connecting-IP", c.cf)
		}
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := h.srv.clientIP(r).String(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestClientCountry(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct{ peer, country, want string }{
		{"127.0.0.1:1", "DE", "DE"},
		{"127.0.0.1:1", "", "??"},
		{"127.0.0.1:1", "D<", "??"},
		{"127.0.0.1:1", "DEU", "??"},
		{"198.51.100.1:1", "DE", "??"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.peer
		r.Header.Set("CF-IPCountry", c.country)
		if got := h.srv.clientCountry(r); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}
