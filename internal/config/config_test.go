package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if *c != (Config{}) {
		t.Fatalf("missing config should be empty, got %+v", c)
	}
}

func TestLoadParsesSubset(t *testing.T) {
	dir := t.TempDir()
	body := `# nt config
[defaults]
priority = "high"
agenda_days = 14   # two weeks
editor = 'nvim'

[web]
port = 8080
host = "0.0.0.0"
edit = true

[tui]
theme = "light"

[ignored]
unknown_key = "skipped"
`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		DefaultPriority: "high",
		AgendaDays:      14,
		Editor:          "nvim",
		WebPort:         8080,
		WebHost:         "0.0.0.0",
		WebEdit:         true,
		TUITheme:        "light",
	}
	if *c != want {
		t.Fatalf("parsed config = %+v, want %+v", *c, want)
	}
}

func TestLoadToleratesGarbage(t *testing.T) {
	dir := t.TempDir()
	// A malformed line must be skipped, not fatal — nt still starts.
	body := "not a kv line\n[web]\nport = 4321\nbroken\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("garbage lines should be skipped, got err %v", err)
	}
	if c.WebPort != 4321 {
		t.Fatalf("valid keys around garbage should still parse, got port %d", c.WebPort)
	}
}
