package tui

import (
	"strings"

	"github.com/navbytes/nt/internal/links"
)

// followTarget is an actionable token (link/tag/project) labeled for keyboard
// activation in follow mode.
type followTarget struct {
	label rune
	kind  string // "link" | "tag" | "project"
	value string
}

// collectTargets gathers the actionable tokens of the selected item and assigns
// each a single-letter label (a, b, c, …), capped at 26.
func (m *Model) collectTargets() []followTarget {
	var raw []followTarget
	add := func(kind, val string) { raw = append(raw, followTarget{kind: kind, value: val}) }

	if m.tab != tabNotes { // tasks or logbook both select a task
		t := m.selectedTask()
		if t == nil {
			return nil
		}
		for _, l := range t.Links() {
			add("link", l)
		}
		for _, tg := range t.Tags() {
			add("tag", tg)
		}
		for _, p := range t.Projects() {
			add("project", p)
		}
	} else {
		n := m.selectedNote()
		if n == nil {
			return nil
		}
		for _, l := range extractLinks(n.Body) {
			add("link", l)
		}
		for _, tg := range n.Tags {
			add("tag", tg)
		}
	}
	for i := range raw {
		if i >= 26 {
			raw = raw[:26]
			break
		}
		raw[i].label = rune('a' + i)
	}
	return raw
}

// extractLinks pulls [[target]] references out of a note body.
func extractLinks(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, "[[")
		if i < 0 {
			break
		}
		j := strings.Index(rest[i+2:], "]]")
		if j < 0 {
			break
		}
		out = append(out, rest[i+2:i+2+j])
		rest = rest[i+2+j+2:]
	}
	return out
}

// startFollow enters follow mode if the selected item has any actionable tokens.
func (m *Model) startFollow() {
	ts := m.collectTargets()
	if len(ts) == 0 {
		m.setStatus("nothing to follow here")
		return
	}
	m.followTargets = ts
	m.followMode = true
}

// handleFollowKey processes a keypress while in follow mode. A lowercase label
// activates the target (links navigate, tags/projects scope); the uppercase
// label regroups by that tag/project instead. Any other key cancels.
func (m *Model) handleFollowKey(key string) {
	m.followMode = false
	m.status = ""
	if key == "esc" {
		return
	}
	for _, ft := range m.followTargets {
		switch key {
		case string(ft.label):
			m.activateTarget(ft, false)
			return
		case strings.ToUpper(string(ft.label)):
			m.activateTarget(ft, true)
			return
		}
	}
}

// activateTarget performs a token's action. group=true selects the regroup
// variant for tags/projects.
func (m *Model) activateTarget(ft followTarget, group bool) {
	switch ft.kind {
	case "link":
		m.gotoLink(ft.value)
	case "tag":
		if group {
			m.groupBy(groupTag, "tag", ft.value)
		} else {
			m.scopeToTag(ft.value)
		}
	case "project":
		if group {
			m.groupBy(groupProject, "project", ft.value)
		} else {
			m.scopeToProject(ft.value)
		}
	}
}

// gotoLink navigates to a [[target]] (note or task), clearing scope/filter so
// the destination is visible.
func (m *Model) gotoLink(target string) {
	d, err := m.eng.Read()
	if err != nil {
		return
	}
	it, ok := links.Resolve(target, d, m.notes)
	if !ok {
		m.setStatus("unresolved link: " + target)
		return
	}
	if it.Kind == "note" {
		m.tab = tabNotes
	} else {
		m.tab = tabTasks
	}
	m.filter, m.scopeTag, m.scopeProject = "", "", ""
	m.offset = 0
	m.rebuild()
	m.selectByID(it.ID)
	m.setStatus("→ " + it.Title)
}

// scopeToTag toggles a @tag scope on the list.
func (m *Model) scopeToTag(tag string) {
	if m.scopeTag == tag {
		m.scopeTag = ""
		m.setStatus("scope cleared")
	} else {
		m.scopeTag = tag
		m.setStatus("scoped to @" + tag)
	}
	m.offset = 0
	m.rebuild()
}

// scopeToProject toggles a +project scope on the list.
func (m *Model) scopeToProject(proj string) {
	if m.scopeProject == proj {
		m.scopeProject = ""
		m.setStatus("scope cleared")
	} else {
		m.scopeProject = proj
		m.setStatus("scoped to +" + proj)
	}
	m.offset = 0
	m.rebuild()
}

// groupBy switches the grouping mode and jumps the cursor to the group holding
// the given tag/project value.
func (m *Model) groupBy(mode groupMode, kind, value string) {
	m.tab = tabTasks
	m.grp = mode
	m.offset = 0
	m.rebuild()
	for i, t := range m.flat {
		if (kind == "tag" && contains(t.Tags(), value)) ||
			(kind == "project" && contains(t.Projects(), value)) {
			m.cursor = i
			break
		}
	}
	m.setStatus("grouped by " + mode.String())
}

// clearScope removes any active scope (used by esc).
func (m *Model) clearScope() bool {
	if m.scopeTag == "" && m.scopeProject == "" {
		return false
	}
	m.scopeTag, m.scopeProject = "", ""
	m.rebuild()
	m.setStatus("scope cleared")
	return true
}
