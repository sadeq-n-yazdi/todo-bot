package clocktest

import (
	"testing"
	"time"
)

func TestFakeAfterFiresOnAdvance(t *testing.T) {
	start := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	f := New(start)
	ch := f.After(time.Minute)
	if f.Waiters() != 1 {
		t.Fatalf("waiters = %d, want 1", f.Waiters())
	}
	f.Advance(30 * time.Second)
	select {
	case <-ch:
		t.Fatal("fired too early")
	default:
	}
	f.Advance(30 * time.Second)
	select {
	case got := <-ch:
		if !got.Equal(start.Add(time.Minute)) {
			t.Fatalf("got %v", got)
		}
	default:
		t.Fatal("did not fire")
	}
	if f.Waiters() != 0 {
		t.Fatalf("waiters = %d, want 0", f.Waiters())
	}
}

func TestFakeAfterNonPositiveFiresImmediately(t *testing.T) {
	f := New(time.Unix(0, 0))
	select {
	case <-f.After(0):
	default:
		t.Fatal("After(0) should fire immediately")
	}
}

func TestFakeSet(t *testing.T) {
	f := New(time.Unix(0, 0))
	ch := f.After(time.Hour)
	f.Set(time.Unix(7200, 0))
	if _, ok := <-ch; !ok {
		t.Fatal("closed")
	}
	if !f.Now().Equal(time.Unix(7200, 0)) {
		t.Fatal("Now not updated")
	}
}
