package note

import (
	"testing"
	"time"
)

func TestValidityRoundTrips(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Token lifetime", "24h access, 7d refresh", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	n.ValidUntil = "2026-01-01"
	n.ValidFrom = "2025-01-01"
	if err := n.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ValidUntil != "2026-01-01" || reloaded.ValidFrom != "2025-01-01" {
		t.Errorf("validity fields didn't round-trip: from=%q until=%q", reloaded.ValidFrom, reloaded.ValidUntil)
	}
}

func TestExpiredAndNotYetValid(t *testing.T) {
	ref := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name            string
		validFrom       string
		validUntil      string
		wantExpired     bool
		wantNotYetValid bool
	}{
		{"no constraints", "", "", false, false},
		{"expired (date form)", "", "2026-01-01", true, false},
		{"expired (RFC3339)", "", "2026-01-01T00:00:00Z", true, false},
		{"still valid", "", "2027-01-01", false, false},
		{"not yet valid", "2027-01-01", "", false, true},
		{"currently valid window", "2026-01-01", "2027-01-01", false, false},
		{"unparseable is ignored", "", "not-a-date", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := &Note{ValidFrom: c.validFrom, ValidUntil: c.validUntil}
			if got := n.Expired(ref); got != c.wantExpired {
				t.Errorf("Expired() = %v, want %v", got, c.wantExpired)
			}
			if got := n.NotYetValid(ref); got != c.wantNotYetValid {
				t.Errorf("NotYetValid() = %v, want %v", got, c.wantNotYetValid)
			}
		})
	}
}
