package cli

import (
	"strings"
	"testing"
	"time"
)

func TestStoreHashEmptyStoreIsStable(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	a := captureRun(t, "store-hash")
	b := captureRun(t, "store-hash")
	if strings.TrimSpace(a) == "" {
		t.Fatal("store-hash printed nothing for an empty store")
	}
	if a != b {
		t.Errorf("store-hash is not stable on an unchanged empty store: %q != %q", a, b)
	}
}

func TestStoreHashChangesOnNoteAddAndIsStableOtherwise(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	before := captureRun(t, "store-hash")

	captureRun(t, "note", "A rule", "--body", "Body.", "--folder", "rules", "--tag", "rule")

	after := captureRun(t, "store-hash")
	if before == after {
		t.Error("store-hash did not change after a note was added")
	}

	// Stable across repeated calls with no intervening change.
	again := captureRun(t, "store-hash")
	if after != again {
		t.Errorf("store-hash changed with no store mutation: %q != %q", after, again)
	}
}

func TestStoreHashChangesOnNoteEdit(t *testing.T) {
	t.Setenv("NT_DIR", t.TempDir())
	captureRun(t, "note", "A rule", "--body", "Body.", "--folder", "rules", "--tag", "rule")
	before := captureRun(t, "store-hash")

	// Force a distinct mtime — some filesystems have coarse mtime resolution.
	time.Sleep(10 * time.Millisecond)
	captureRun(t, "edit", "A rule", "--append", "\nMore detail.")

	after := captureRun(t, "store-hash")
	if before == after {
		t.Error("store-hash did not change after a note edit")
	}
}
