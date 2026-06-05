// Package search implements files-only search (SPEC §7.1): ripgrep when a real
// rg binary is on PATH, otherwise a built-in walker. Used for note full-text
// search and for backlink scanning. Task list/filter/sort do NOT go through
// here — those parse the whole tasks file into structs (see cmd list).
package search

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Hit is one matching line.
type Hit struct {
	Path string
	Line int
	Text string
}

// HasRipgrep reports whether a real rg executable is available.
func HasRipgrep() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

// Literal searches paths (files or directories) for a fixed substring,
// case-insensitively. It prefers ripgrep and falls back to a built-in walk.
func Literal(query string, paths ...string) ([]Hit, error) {
	if HasRipgrep() {
		if hits, err := ripgrep(query, paths...); err == nil {
			return hits, nil
		}
		// fall through to the walker on any rg error
	}
	return walk(query, paths...)
}

func ripgrep(query string, paths ...string) ([]Hit, error) {
	args := append([]string{"--no-heading", "--line-number", "--color=never", "--fixed-strings", "--ignore-case", query}, paths...)
	out, err := exec.Command("rg", args...).Output()
	if err != nil {
		// rg exits 1 when there are no matches — that's not an error for us.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var hits []Hit
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		hits = append(hits, parseRgLine(sc.Text()))
	}
	return hits, nil
}

// parseRgLine parses "path:line:text".
func parseRgLine(s string) Hit {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 3 {
		return Hit{Text: s}
	}
	return Hit{Path: parts[0], Line: atoi(parts[1]), Text: parts[2]}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// walk is the dependency-free fallback: scan files (recursing into dirs) for
// the query substring, case-insensitively.
func walk(query string, paths ...string) ([]Hit, error) {
	needle := strings.ToLower(query)
	var hits []Hit
	scan := func(path string) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		ln := 0
		for sc.Scan() {
			ln++
			if strings.Contains(strings.ToLower(sc.Text()), needle) {
				hits = append(hits, Hit{Path: path, Line: ln, Text: sc.Text()})
			}
		}
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			filepath.WalkDir(p, func(fp string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					scan(fp)
				}
				return nil
			})
		} else {
			scan(p)
		}
	}
	return hits, nil
}
