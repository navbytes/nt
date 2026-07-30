package note

import "testing"

func mkExact(rel, title string) *Note {
	return &Note{Rel: rel, Title: title}
}

func TestFindExact(t *testing.T) {
	notes := []*Note{
		mkExact("jwt-token-lifetime.md", "JWT token lifetime"),
		mkExact("proja/setup-guide.md", "Setup Guide"),
		mkExact("__tasks__/x.md", "Setup Guide"), // reserved — never a match
	}
	if got := FindExact(notes, "jwt TOKEN lifetime", ""); got == nil || got.Rel != "jwt-token-lifetime.md" {
		t.Fatalf("case-insensitive title should match, got %v", got)
	}
	// Slug match even when the stored title diverges from the filename.
	if got := FindExact(notes, "JWT token lifetime", ""); got == nil {
		t.Fatal("slug match failed")
	}
	// Folder scoping.
	if got := FindExact(notes, "Setup Guide", "projb"); got != nil {
		t.Fatalf("wrong folder must not match, got %v", got.Rel)
	}
	if got := FindExact(notes, "Setup Guide", "proja"); got == nil || got.Rel != "proja/setup-guide.md" {
		t.Fatalf("folder-scoped match failed, got %v", got)
	}
	// Store-wide skips the reserved copy but finds the real one.
	if got := FindExact(notes, "setup guide", ""); got == nil || got.Rel != "proja/setup-guide.md" {
		t.Fatalf("store-wide match should skip reserved notes, got %v", got)
	}
	if got := FindExact(notes, "completely new", ""); got != nil {
		t.Fatalf("no-match should return nil, got %v", got.Rel)
	}
}
