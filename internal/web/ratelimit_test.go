package web

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
)

func TestLimiterRefill(t *testing.T) {
	clk := clocktest.New(testNow)
	l := newLimiter(clk, 2, time.Minute)
	for i := range 2 {
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("token %d", i)
		}
	}
	ok, retry := l.allow("a")
	if ok || retry != 30*time.Second {
		t.Fatalf("empty bucket: %v, retry %v", ok, retry)
	}
	if ok, _ := l.allow("b"); !ok {
		t.Fatal("another key")
	}
	clk.Advance(30 * time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("after one refill period")
	}
	if ok, _ := l.allow("a"); ok {
		t.Fatal("only one token came back")
	}
	clk.Advance(time.Hour)
	for range 2 {
		if ok, _ := l.allow("a"); !ok {
			t.Fatal("a bucket holds at most n tokens")
		}
	}
	if ok, _ := l.allow("a"); ok {
		t.Fatal("a bucket holds more than n tokens")
	}
}

func TestLimiterEvictsOldestKey(t *testing.T) {
	clk := clocktest.New(testNow)
	l := newLimiter(clk, 1, time.Hour)
	l.maxKeys = 3
	for _, k := range []string{"a", "b", "c"} {
		l.allow(k)
	}
	l.allow("a") // a is now the newest; b is the oldest
	l.allow("d") // removes b
	if len(l.keys) != 3 {
		t.Fatalf("%d keys", len(l.keys))
	}
	if _, ok := l.keys["b"]; ok {
		t.Error("the oldest key was kept")
	}
	if ok, _ := l.allow("b"); !ok {
		t.Error("a removed key starts with a full bucket")
	}
	if ok, _ := l.allow("d"); ok {
		t.Error("a kept key lost its state")
	}
	if maxLimiterKeys != 10000 {
		t.Errorf("maxLimiterKeys %d", maxLimiterKeys)
	}
}
