package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
)

// startYank arms the yank chord (next key: y=id, l=line, t=text).
func (m *Model) startYank() {
	if len(m.targets()) == 0 {
		return
	}
	m.yankPending = true
}

// handleYankKey resolves the second key of a yank chord.
func (m *Model) handleYankKey(key string) {
	m.yankPending = false
	switch key {
	case "y":
		m.yank("id")
	case "l":
		m.yank("line")
	case "t":
		m.yank("text")
	default:
		m.status = ""
	}
}

// yankString builds the clipboard payload for the targets: short ids are
// space-joined (paste into `nt done id1 id2`), lines/text newline-joined.
func (m *Model) yankString(kind string) string {
	d, err := m.eng.Read()
	if err != nil {
		return ""
	}
	var parts []string
	for _, id := range m.targets() {
		tk := d.FindByID(id)
		if tk == nil {
			continue
		}
		switch kind {
		case "id":
			parts = append(parts, shortCode(id))
		case "line":
			parts = append(parts, tk.Line())
		case "text":
			parts = append(parts, tk.Text)
		}
	}
	sep := "\n"
	if kind == "id" {
		sep = " "
	}
	return strings.Join(parts, sep)
}

// yank copies the targets to the clipboard, surfacing failure loudly (clipboard
// tools are often absent over SSH/headless).
func (m *Model) yank(kind string) {
	s := m.yankString(kind)
	if s == "" {
		return
	}
	if err := clipboard.WriteAll(s); err != nil {
		m.setStatus("clipboard unavailable (no pbcopy/xclip/wl-copy)")
		return
	}
	n := len(strings.Split(s, "\n"))
	if kind == "id" {
		n = len(strings.Fields(s))
	}
	noun := kind
	if n > 1 {
		noun += "s"
	}
	m.setStatus(fmt.Sprintf("yanked %d %s", n, noun))
}

// shortCode is the displayed 6-char ULID suffix (CLI resolves it as a handle).
func shortCode(id string) string {
	if len(id) > 6 {
		return id[len(id)-6:]
	}
	return id
}
