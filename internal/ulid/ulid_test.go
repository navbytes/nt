package ulid

import (
	"testing"
	"time"
)

func TestTimeRoundTrip(t *testing.T) {
	before := time.Now()
	id := New()
	got, ok := Time(id)
	if !ok {
		t.Fatalf("Time(%q) not ok", id)
	}
	// The decoded timestamp is millisecond-truncated and must bracket "now".
	if got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Fatalf("Time(%q) = %v, want ≈ %v", id, got, before)
	}
}

func TestTimeOrdering(t *testing.T) {
	a := New()
	time.Sleep(2 * time.Millisecond)
	b := New()
	ta, _ := Time(a)
	tb, _ := Time(b)
	if !tb.After(ta) {
		t.Fatalf("later ULID should decode to a later time: %v !> %v", tb, ta)
	}
}

func TestTimeInvalid(t *testing.T) {
	if _, ok := Time("short"); ok {
		t.Error("too-short id should not decode")
	}
	if _, ok := Time("IIIIIIIIII"); ok { // I is not in the Crockford alphabet
		t.Error("invalid chars should not decode")
	}
}
