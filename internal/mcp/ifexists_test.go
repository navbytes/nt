package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// if_exists:"return" on an exact title match must write NOTHING and hand back
// the existing note's id + mtime token so the agent edits it in place — the
// delta-write steering of the memory-dynamics spec (§4).
func TestMCPNoteIfExistsReturn(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "JWT token lifetime", "body": "24h"})
	if err != nil {
		t.Fatal(err)
	}
	var created noteOut
	json.Unmarshal([]byte(out), &created)

	out, err = s.dispatch("nt_note", map[string]any{"title": "jwt TOKEN lifetime", "if_exists": "return"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Matched bool   `json:"matched"`
		ID      string `json:"id"`
		MTime   string `json:"mtime"`
		Hint    string `json:"hint"`
	}
	json.Unmarshal([]byte(out), &res)
	if !res.Matched || res.ID != created.ID {
		t.Fatalf("expected matched=true with the original id %s, got %s", created.ID, out)
	}
	if res.MTime == "" {
		t.Fatalf("matched response must include the mtime token for expect_mtime round-trips: %s", out)
	}
	if !strings.Contains(res.Hint, "nt_note_edit") {
		t.Fatalf("hint should steer to nt_note_edit, got %q", res.Hint)
	}
	// Nothing was written: the store still holds exactly one active note.
	if n := len(s.listNotes()); n != 1 {
		t.Fatalf("if_exists=return created a note anyway: %d notes on disk", n)
	}
}

// No match → create exactly as today, whatever the mode.
func TestMCPNoteIfExistsNoMatchCreates(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "brand new topic", "if_exists": "return"})
	if err != nil {
		t.Fatal(err)
	}
	var created noteOut
	json.Unmarshal([]byte(out), &created)
	if created.ID == "" {
		t.Fatalf("no-match if_exists=return should create, got %s", out)
	}
}

// "error" refuses loudly; an invalid mode is rejected by name.
func TestMCPNoteIfExistsErrorAndInvalid(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "Some Topic Here"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.dispatch("nt_note", map[string]any{"title": "some topic here", "if_exists": "error"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("if_exists=error should refuse on a match, got %v", err)
	}
	_, err = s.dispatch("nt_note", map[string]any{"title": "x y", "if_exists": "upsert"})
	if err == nil || !strings.Contains(err.Error(), "if_exists") {
		t.Fatalf("invalid mode should be rejected, got %v", err)
	}
}

// Archived and superseded notes must never match — a consolidation decision
// can't resurrect through the if_exists side door (spec §4.1).
func TestMCPNoteIfExistsSkipsRetired(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "Retired Topic Note"})
	if err != nil {
		t.Fatal(err)
	}
	var old noteOut
	json.Unmarshal([]byte(out), &old)
	if _, err := s.dispatch("nt_archive", map[string]any{"handle": old.ID}); err != nil {
		t.Fatal(err)
	}
	out, err = s.dispatch("nt_note", map[string]any{"title": "retired topic note", "if_exists": "return", "force": true})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Matched bool   `json:"matched"`
		ID      string `json:"id"`
	}
	json.Unmarshal([]byte(out), &res)
	if res.Matched {
		t.Fatalf("archived note must not match if_exists, got %s", out)
	}
	if res.ID == "" || res.ID == old.ID {
		t.Fatalf("expected a fresh note, got %s", out)
	}
}

// Folder scoping: a same-titled note in another folder is not a match.
func TestMCPNoteIfExistsFolderScope(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{"title": "Setup Guide", "folder": "proja"}); err != nil {
		t.Fatal(err)
	}
	out, err := s.dispatch("nt_note", map[string]any{"title": "Setup Guide", "folder": "projb", "if_exists": "return", "force": true})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Matched bool   `json:"matched"`
		ID      string `json:"id"`
	}
	json.Unmarshal([]byte(out), &res)
	if res.Matched {
		t.Fatalf("folder-scoped if_exists must not match across folders, got %s", out)
	}
	// Store-wide (no folder) DOES match either copy.
	out, err = s.dispatch("nt_note", map[string]any{"title": "setup guide", "if_exists": "return"})
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(out), &res)
	if !res.Matched {
		t.Fatalf("store-wide if_exists should match an existing copy, got %s", out)
	}
}

// Low-confidence recall carries the escalate hint; a store-relevant query does not.
func TestMCPRecallEscalate(t *testing.T) {
	s := newServer(t)
	if _, err := s.dispatch("nt_note", map[string]any{
		"title": "JWT refresh window", "description": "access tokens expire after a day", "tags": []any{"auth"},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := s.dispatch("nt_recall", map[string]any{"context": "kubernetes ingress annotations"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Escalate *struct {
			Reason string           `json:"reason"`
			Try    []map[string]any `json:"try"`
		} `json:"escalate"`
	}
	json.Unmarshal([]byte(out), &res)
	if res.Escalate == nil {
		t.Fatalf("unrelated query should escalate, got %s", out)
	}
	if res.Escalate.Reason != "no_results" && res.Escalate.Reason != "low_confidence" {
		t.Fatalf("unexpected escalate reason %q", res.Escalate.Reason)
	}
	if len(res.Escalate.Try) == 0 {
		t.Fatalf("escalate must suggest concrete next calls, got %s", out)
	}

	out, err = s.dispatch("nt_recall", map[string]any{"context": "JWT refresh window for auth tokens"})
	if err != nil {
		t.Fatal(err)
	}
	res.Escalate = nil // Unmarshal leaves absent fields untouched — reset before re-decoding
	json.Unmarshal([]byte(out), &res)
	if res.Escalate != nil {
		t.Fatalf("a strong on-topic hit must not escalate, got %s", out)
	}
}

// include_archived reaches retired notes and flags them.
func TestMCPSearchIncludeArchived(t *testing.T) {
	s := newServer(t)
	out, err := s.dispatch("nt_note", map[string]any{"title": "old dead-end approach", "body": "tried zookeeper"})
	if err != nil {
		t.Fatal(err)
	}
	var n noteOut
	json.Unmarshal([]byte(out), &n)
	if _, err := s.dispatch("nt_archive", map[string]any{"handle": n.ID}); err != nil {
		t.Fatal(err)
	}
	out, err = s.dispatch("nt_search", map[string]any{"query": "zookeeper"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Notes []noteStub `json:"notes"`
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Notes) != 0 {
		t.Fatalf("default search must not surface archived notes, got %s", out)
	}
	out, err = s.dispatch("nt_search", map[string]any{"query": "zookeeper", "include_archived": true})
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(out), &res)
	if len(res.Notes) != 1 || !res.Notes[0].Archived {
		t.Fatalf("include_archived should surface the archived note flagged, got %s", out)
	}
}
