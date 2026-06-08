package task

import "testing"

func TestResolveExactAndShortCode(t *testing.T) {
	doc := Parse([]byte("ship it id:01ARZ3NDEKTSV4RRFFQ69G5FAV\n"))
	full := "01ARZ3NDEKTSV4RRFFQ69G5FAV"

	if got, amb := doc.Resolve(full); amb || got == nil {
		t.Fatalf("full id should resolve exactly, got (%v, amb=%v)", got, amb)
	}
	// The displayed handle is the trailing short code (last 6 chars).
	if got, amb := doc.Resolve("G5FAV"); amb || got == nil {
		t.Fatalf("trailing short code should resolve, got (%v, amb=%v)", got, amb)
	}
	// Lowercase is accepted (handles are upper-cased before matching).
	if got, _ := doc.Resolve("g5fav"); got == nil {
		t.Error("lowercase short code should resolve")
	}
}

// TestResolveSuffixNotPrefix is the F5/C5 regression: a handle that is task A's
// trailing short code must resolve to A even when it is also a leading prefix of
// task B — prefix-matching B previously made A's own handle ambiguous (or could
// hit B). Resolution is anchored to the displayed suffix.
func TestResolveSuffixNotPrefix(t *testing.T) {
	// A ends with "FFQABC"; B starts with "FFQABC".
	doc := Parse([]byte(
		"task a id:01ARZ3NDEKTSV4RRFFQABC\n" +
			"task b id:FFQABCNDEKTSV4RRFFQ69G\n"))

	got, amb := doc.Resolve("FFQABC")
	if amb {
		t.Fatal("FFQABC is A's suffix handle — prefix-matching B must not make it ambiguous")
	}
	if got == nil || got.Text != "task a" {
		t.Fatalf("FFQABC should resolve to task a (suffix), got %v", got)
	}

	// A non-existent suffix resolves to nothing (not to B via prefix).
	if got, amb := doc.Resolve("FFQABCNDEK"); got != nil || amb {
		t.Errorf("a leading-prefix-only handle must not resolve, got (%v, amb=%v)", got, amb)
	}
}

func TestResolveAmbiguousSuffix(t *testing.T) {
	// Two ids sharing the same trailing short code → ambiguous, never a silent pick.
	doc := Parse([]byte("a id:01ARZ3NDEKTSV4RRFFQ69G5FAV\nb id:01BXYZNDEKTSV4RR000069G5FAV\n"))
	if got, amb := doc.Resolve("G5FAV"); !amb || got != nil {
		t.Fatalf("a colliding suffix should be ambiguous, got (%v, amb=%v)", got, amb)
	}
}
