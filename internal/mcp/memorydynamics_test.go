package mcp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// nt_touch stamps reviewed: today and hands back a fresh mtime token.
func TestMCPTouch(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "volatile fact", "half_life": "30d"})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	if n.HalfLife != "30d" {
		t.Fatalf("half_life not persisted at create: %s", out)
	}
	out, err = s.dispatch("nt_touch", map[string]any{"id": n.ID})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Reviewed string `json:"reviewed"`
		MTime    string `json:"mtime"`
	}
	json.Unmarshal([]byte(out), &res)
	if res.Reviewed == "" || res.MTime == "" {
		t.Fatalf("touch should return reviewed + mtime, got %s", out)
	}
	gout, _ := s.dispatch("nt_get", map[string]any{"handle": n.ID})
	if !strings.Contains(gout, res.Reviewed) {
		t.Fatalf("reviewed not visible via nt_get: %s", gout)
	}
	// The returned token must survive the documented round-trip: it has to
	// describe the POST-save file, or every chained expect_mtime edit fails
	// with a spurious staleness error.
	if _, err := s.dispatch("nt_note_edit", map[string]any{"id": n.ID, "append": "still true", "expect_mtime": res.MTime}); err != nil {
		t.Fatalf("touch's mtime token should be valid for an immediate expect_mtime edit: %v", err)
	}
}

// A bad half_life is rejected at write time (never silently stored broken).
func TestMCPHalfLifeValidation(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "x y", "half_life": "soonish"}); err == nil {
		t.Fatal("bad half_life should be rejected")
	}
	out, err := s.dispatch("nt_note", map[string]any{"title": "x y", "half_life": "none"})
	if err != nil {
		t.Fatalf("half_life none is a valid opt-out: %v", err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	if _, err := s.dispatch("nt_note_edit", map[string]any{"id": n.ID, "reviewed": "not-a-date"}); err == nil {
		t.Fatal("bad reviewed should be rejected")
	}
}

// nt_decide appends the dated line; a second decide prepends above it.
func TestMCPDecide(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "auth approach", "body": "JWT."})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	out, err = s.dispatch("nt_decide", map[string]any{"id": n.ID, "text": "chose JWT over sessions — stateless workers"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Decisions int    `json:"decisions"`
		Latest    string `json:"latestDecision"`
	}
	json.Unmarshal([]byte(out), &res)
	if res.Decisions != 1 || res.Latest == "" {
		t.Fatalf("decide result: %s", out)
	}
	// Hostile input is refused.
	if _, err := s.dispatch("nt_decide", map[string]any{"id": n.ID, "text": "a\nb"}); err == nil {
		t.Fatal("multi-line decision text should be refused")
	}
	// The section is ordinary body — visible via nt_get.
	gout, _ := s.dispatch("nt_get", map[string]any{"handle": n.ID})
	if !strings.Contains(gout, "Decisions") || !strings.Contains(gout, "stateless workers") {
		t.Fatalf("decision not in body: %s", gout)
	}
}

// nt_note supersede: stamps a provenance decision line on the NEW note.
func TestMCPSupersedeStampsDecision(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "old approach detail", "body": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	var old noteOut
	json.Unmarshal([]byte(out), &old)
	out, err = s.dispatch("nt_note", map[string]any{"title": "new approach detail", "body": "v2", "supersede": old.ID})
	if err != nil {
		t.Fatal(err)
	}
	var neu noteOut
	json.Unmarshal([]byte(out), &neu)
	gout, _ := s.dispatch("nt_get", map[string]any{"handle": neu.ID})
	if !strings.Contains(gout, "supersedes [[old-approach-detail]]") {
		t.Fatalf("new note should carry the supersedes stamp: %s", gout)
	}
}

// old_string that lives in the DESCRIPTION (not the body) gets a targeted
// error naming the real field — the simulation showed agents guessing.
func TestMCPNoteEditOldStringInDescription(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "desc target note", "description": "needs APP_ENV=prod exported", "body": "other text"})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	_, err = s.dispatch("nt_note_edit", map[string]any{"id": n.ID, "old_string": "APP_ENV=prod", "new_string": "APP_ENV=dev"})
	if err == nil || !strings.Contains(err.Error(), "DESCRIPTION") {
		t.Fatalf("should point at the description field, got %v", err)
	}
	// Truly-absent text keeps the plain not-found error (with the desc pointer).
	_, err = s.dispatch("nt_note_edit", map[string]any{"id": n.ID, "old_string": "nowhere at all", "new_string": "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("plain not-found expected, got %v", err)
	}
}

// nt_archive superseded_by= must stamp the same provenance line as every
// other supersede door — history must not depend on which tool performed it.
func TestMCPArchiveSupersededByStampsDecision(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "dup capture here", "body": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	var old noteOut
	json.Unmarshal([]byte(out), &old)
	out, err = s.dispatch("nt_note", map[string]any{"title": "kept canonical note", "body": "v2", "force": true})
	if err != nil {
		t.Fatal(err)
	}
	var kept noteOut
	json.Unmarshal([]byte(out), &kept)
	if _, err := s.dispatch("nt_archive", map[string]any{"handle": old.ID, "superseded_by": kept.ID}); err != nil {
		t.Fatal(err)
	}
	gout, _ := s.dispatch("nt_get", map[string]any{"handle": kept.ID})
	if !strings.Contains(gout, "supersedes [[dup-capture-here]]") {
		t.Fatalf("archive-with-superseded_by should stamp the keeper: %s", gout)
	}
}

// nt_history: honest error without git; real history with it.
func TestMCPHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "tracked note", "body": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	if _, err := s.dispatch("nt_history", map[string]any{"id": n.ID}); err == nil || !strings.Contains(err.Error(), "git-init") {
		t.Fatalf("no-git store should point at nt git-init, got %v", err)
	}
	dir := s.eng.S.Dir
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init")
	run("add", "-A")
	run("commit", "-m", "capture tracked note")
	out, err = s.dispatch("nt_history", map[string]any{"id": n.ID})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		History string `json:"history"`
	}
	json.Unmarshal([]byte(out), &res)
	if !strings.Contains(res.History, "capture tracked note") {
		t.Fatalf("history should show the commit, got %s", out)
	}
}

// Decay is visible end-to-end: a faded note is flagged in recall + ranked below
// a fresh equal-relevance note. The faded note is written to disk directly with
// old dates + an old file mtime — the API path can't fabricate age, because the
// age basis is the LATEST of reviewed/updated/created/mtime (any real edit or
// re-confirmation resets the clock, by design).
func TestMCPRecallDecay(t *testing.T) {
	s := newServer(t)
	old := "---\nid: 01HZZZZZZZZZZZZZZZZZZZZZZF\ntags: [cache]\ndescription: redis cache eviction policy\ncreated: 2020-01-01T00:00:00Z\nhalf_life: 7d\n---\n\n# redis eviction gotcha old\n\nstale detail\n"
	fadedPath := filepath.Join(s.eng.S.NotesDir(), "redis-eviction-gotcha-old.md")
	if err := os.WriteFile(fadedPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(fadedPath, past, past); err != nil {
		t.Fatal(err)
	}
	fadedID := "01HZZZZZZZZZZZZZZZZZZZZZZF"
	out, err := s.dispatch("nt_note", map[string]any{"title": "redis eviction gotcha new", "description": "redis cache eviction policy", "tags": []any{"cache"}, "force": true})
	if err != nil {
		t.Fatal(err)
	}
	var fresh noteOut
	json.Unmarshal([]byte(out), &fresh)
	freshID := fresh.ID
	out, err = s.dispatch("nt_recall", map[string]any{"context": "redis cache eviction policy"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Results []struct {
			ID    string `json:"id"`
			Faded bool   `json:"faded"`
		} `json:"results"`
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Results) < 2 {
		t.Fatalf("want both notes, got %s", out)
	}
	if res.Results[0].ID != freshID {
		t.Fatalf("fresh note should outrank the faded twin, got %s first (%s)", res.Results[0].ID, out)
	}
	for _, r := range res.Results {
		if r.ID == fadedID && !r.Faded {
			t.Fatalf("faded note should be flagged: %s", out)
		}
		if r.ID == freshID && r.Faded {
			t.Fatalf("fresh note must not be flagged faded: %s", out)
		}
	}
}
