package note

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSaveIfUnchangedNoExpectBehavesLikeSave(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Plain save", "body", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	n.Body = "edited"
	if err := n.SaveIfUnchanged(""); err != nil {
		t.Fatalf("SaveIfUnchanged with no expect token should behave like Save: %v", err)
	}
	reloaded, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Save() re-prepends "# Title" since the new body doesn't start with a
	// heading — the same behavior plain Save has always had; assert on content,
	// not exact bytes.
	if !strings.Contains(reloaded.Body, "edited") {
		t.Errorf("body = %q, want it to contain %q", reloaded.Body, "edited")
	}
}

func TestSaveIfUnchangedRefusesOnDrift(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Concurrent edit", "original", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	token := loaded.MTimeToken()
	if token == "" {
		t.Fatal("expected a non-empty mtime token for a loaded note")
	}

	// Simulate a concurrent writer: touch the file with a distinct mtime.
	time.Sleep(10 * time.Millisecond)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(n.Path, future, future); err != nil {
		t.Fatal(err)
	}

	loaded.Body = "clobbered?"
	err = loaded.SaveIfUnchanged(token)
	if err == nil {
		t.Fatal("expected SaveIfUnchanged to refuse when the file's mtime drifted")
	}
	var stale *StaleNoteError
	if !errors.As(err, &stale) {
		t.Fatalf("expected a *StaleNoteError, got %T: %v", err, err)
	}

	// The file must be untouched by the refused write.
	reloaded, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Body == "clobbered?" {
		t.Error("SaveIfUnchanged wrote despite refusing — the file was clobbered")
	}
}

func TestSaveIfUnchangedSucceedsWhenTokenMatches(t *testing.T) {
	s := testStore(t)
	n, err := Create(s, "Uncontended edit", "original", nil, "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	token := loaded.MTimeToken()

	loaded.Body = "updated"
	if err := loaded.SaveIfUnchanged(token); err != nil {
		t.Fatalf("expected the matching token to succeed: %v", err)
	}
	reloaded, err := Load(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reloaded.Body, "updated") {
		t.Errorf("body = %q, want it to contain %q", reloaded.Body, "updated")
	}
}

func TestMTimeTokenEmptyForUnloadedNote(t *testing.T) {
	n := &Note{Path: "/tmp/does-not-matter.md"}
	if tok := n.MTimeToken(); tok != "" {
		t.Errorf("MTimeToken() = %q for a zero-value ModTime, want empty", tok)
	}
}
