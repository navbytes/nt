package note

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Relevance decay (memory-dynamics spec §3): a note carrying a half_life fades
// smoothly as it ages un-reconfirmed — down-ranked in recall and rolled out of
// the index's recent tier, never hidden. The smooth complement to the
// valid_until cliff: use valid_until when you KNOW the expiry, half_life when
// you only know the fact goes stale ("config gotchas rot in ~90d").
const (
	// DecayFloor is the minimum decay multiplier — a fully-faded note is
	// down-weighted, never zeroed (same spirit as recall's expiredPenalty:
	// fade, never hide). Spec open question #1: revisit against a real
	// store's age histogram.
	DecayFloor = 0.30
	// FadedThreshold marks a note as ~faded once its decay factor drops below
	// this — one half-life elapsed. A display/tiering signal, not a filter.
	FadedThreshold = 0.5
)

// ParseHalfLife parses a half_life value: Nd (days), Nw (weeks), Nm (months,
// 30d), Ny (years, 365d), or "none" (explicit opt-out, isNone=true). ok=false
// for "", zero/negative N, or anything unparseable — treated everywhere as
// "no decay", surfaced only by doctor (a bad value must never zero a note).
func ParseHalfLife(s string) (d time.Duration, ok, isNone bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, false, false
	}
	if s == "none" {
		return 0, false, true
	}
	unit := s[len(s)-1]
	nStr := s[:len(s)-1]
	n, err := strconv.Atoi(nStr)
	if err != nil || n <= 0 {
		return 0, false, false
	}
	day := 24 * time.Hour
	switch unit {
	case 'd':
		return time.Duration(n) * day, true, false
	case 'w':
		return time.Duration(n) * 7 * day, true, false
	case 'm':
		return time.Duration(n) * 30 * day, true, false
	case 'y':
		return time.Duration(n) * 365 * day, true, false
	}
	return 0, false, false
}

// ParseFlexDate parses a YYYY-MM-DD or RFC3339 date — the exported form of
// the validity-date parser, for callers validating reviewed:/valid_* input.
func ParseFlexDate(s string) (time.Time, bool) { return parseValidityDate(s) }

// AgeBasis is the reference date decay measures from: the latest of
// reviewed/updated/created (frontmatter) and the file mtime. Reviewing a note
// (nt touch) resets the clock without editing it — confirming a fact is still
// true is real information. Zero time when nothing is parseable.
func (n *Note) AgeBasis() time.Time {
	var best time.Time
	for _, s := range []string{n.Reviewed, n.Updated, n.Created} {
		if t, ok := parseValidityDate(s); ok && t.After(best) {
			best = t
		}
	}
	if n.ModTime.After(best) {
		best = n.ModTime
	}
	return best
}

// Decay returns the note's relevance multiplier as of now:
// max(DecayFloor, 0.5^(age/halfLife)). 1.0 when half_life is unset, "none",
// unparseable, or the age basis is unknown — decay is strictly opt-in and a
// bad value can never hurt ranking.
func (n *Note) Decay(now time.Time) float64 {
	hl, ok, _ := ParseHalfLife(n.HalfLife)
	if !ok {
		return 1.0
	}
	basis := n.AgeBasis()
	if basis.IsZero() {
		return 1.0
	}
	age := now.Sub(basis)
	if age <= 0 {
		return 1.0
	}
	f := math.Pow(0.5, float64(age)/float64(hl))
	if f < DecayFloor {
		return DecayFloor
	}
	return f
}

// Faded reports whether the note has aged past one half-life un-reconfirmed —
// the display/tiering signal behind the ~faded chip and the index rollup.
func (n *Note) Faded(now time.Time) bool {
	hl, ok, _ := ParseHalfLife(n.HalfLife)
	if !ok {
		return false
	}
	basis := n.AgeBasis()
	if basis.IsZero() {
		return false
	}
	return now.Sub(basis) > hl // decay < 0.5 ⟺ age > one half-life
}

// FadedDays is how many days past its half-life the note is (0 when not
// faded) — the sort key for the review report's most-faded-first listing.
func (n *Note) FadedDays(now time.Time) int {
	hl, ok, _ := ParseHalfLife(n.HalfLife)
	if !ok {
		return 0
	}
	basis := n.AgeBasis()
	if basis.IsZero() {
		return 0
	}
	over := now.Sub(basis) - hl
	if over <= 0 {
		return 0
	}
	return int(over / (24 * time.Hour))
}
