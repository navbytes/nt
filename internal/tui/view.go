package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/navbytes/nt/internal/links"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	if m.help {
		return m.helpView()
	}
	header := m.headerView()
	footer := m.footerView()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 1 {
		bodyH = 1
	}

	var body string
	switch {
	case m.detailFocus && m.width < wideMin:
		body = m.detailCard(bodyH)
	case m.width >= wideMin:
		body = m.wideView(bodyH)
	case m.width <= compactMax:
		body = m.compactView(bodyH)
	default:
		body = m.standardView(bodyH)
	}
	// Pad the body to the full available height so the footer pins to the
	// bottom of the window instead of floating under the content.
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// --- header / footer -----------------------------------------------------

func (m *Model) headerView() string {
	// Worded tabs when there's room; bare numbers in compact widths so the labels
	// never crowd out (or overflow past) the metadata.
	full := m.width > compactMax
	tab := func(active bool, long, short string) string {
		s := short
		if full {
			s = long
		}
		if active {
			return stTabOn.Render(s)
		}
		return stTabOff.Render(s)
	}
	t1 := tab(m.tab == tabTasks, " 1 tasks ", " 1 ")
	t2 := tab(m.tab == tabNotes, " 2 notes ", " 2 ")
	t3 := tab(m.tab == tabLogbook, " 3 log ", " 3 ")
	p1, brand, p2 := stHeader.Render("  "), stBrand.Render(" nt "), stHeader.Render("  ")
	left := p1 + brand + p2 + t1 + t2 + t3
	// Record the clickable tab-label column ranges (header row 0) for the mouse.
	tabStart := lipgloss.Width(p1) + lipgloss.Width(brand) + lipgloss.Width(p2)
	w1, w2, w3 := lipgloss.Width(t1), lipgloss.Width(t2), lipgloss.Width(t3)
	m.tabHits = []tabHit{
		{start: tabStart, end: tabStart + w1, tab: tabTasks},
		{start: tabStart + w1, end: tabStart + w1 + w2, tab: tabNotes},
		{start: tabStart + w1 + w2, end: tabStart + w1 + w2 + w3, tab: tabLogbook},
	}

	// The right side has two tiers. CONTEXT (group mode, toggles, done count) is
	// plain dim text and is dropped first when space is tight. STATE chips (lock,
	// filter, scope, marks) are bright badges, kept as long as they fit. Tiering +
	// measuring guarantees the header never exceeds its width — previously the
	// compact header overflowed and rendered wider than the body.
	sp := stBarBg.Render("  ")

	context := stBarBg.Render("group:" + m.grp.String())
	if m.showBlocked {
		context += stBarBg.Render("  ·  +blocked")
	}
	if m.tab == tabTasks {
		if m.showDone {
			context += sp + chip("✓ done shown", cGreen)
		} else if dc := m.doneCount(); dc > 0 {
			context += sp + stGreen.Background(cBarBg).Render(" ✓ ") +
				stBarBg.Render(fmt.Sprintf("%d done ", dc))
		}
	}

	state := ""
	if m.filter != "" {
		state += sp + chip(fmt.Sprintf("⊃ filter: %s · %d", m.filter, m.selectableLen()), cOrange)
	}
	if m.scopeTag != "" {
		state += sp + chip("@"+m.scopeTag, cMagenta)
	}
	if m.scopeProject != "" {
		state += sp + chip("+"+m.scopeProject, cBlue)
	}
	if len(m.marked) > 0 {
		lbl := fmt.Sprintf("● %d marked", len(m.marked))
		if h := m.hiddenMarked(); h > 0 {
			lbl += fmt.Sprintf(" · %d hidden", h)
		}
		state += sp + chip(lbl, cYellow)
	}
	lockChip := ""
	if m.locked {
		lockChip = sp + chip("LOCKED", cRed)
		state += lockChip
	}

	// Choose the richest right side that fits (reserve a 2-col gap + 2-col margin).
	leftW := lipgloss.Width(left)
	avail := m.width - leftW - 4
	right := context + state
	if avail < 1 || lipgloss.Width(right) > avail {
		right = state // drop low-priority context first
		if lipgloss.Width(right) > avail {
			if m.locked && lipgloss.Width(lockChip) <= avail {
				right = lockChip // at the extreme, the lock indicator still wins
			} else {
				right = ""
			}
		}
	}

	var line string
	if lipgloss.Width(right) == 0 {
		line = left + barPad(m.width-leftW)
	} else {
		pad := m.width - leftW - lipgloss.Width(right) - 2
		if pad < 1 {
			pad = 1
		}
		line = left + barPad(pad) + right + barPad(2)
	}
	rule := stRule.Render(strings.Repeat("─", m.width))
	return line + "\n" + rule
}

func (m *Model) footerView() string {
	rule := stRule.Render(strings.Repeat("─", m.width))
	var content string
	if m.yankPending {
		content = stKeyBg.Render("  yank ") + stBarBg.Render(" ") +
			stKeyBg.Render("y") + stBarBg.Render(" id   ") +
			stKeyBg.Render("l") + stBarBg.Render(" line   ") +
			stKeyBg.Render("t") + stBarBg.Render(" text   ") +
			lipgloss.NewStyle().Foreground(cDim).Background(cBarBg).Render("esc")
	} else if m.confirm != nil {
		content = lipgloss.NewStyle().Foreground(cOrange).Background(cBarBg).Render("  "+m.confirm.prompt+" ") +
			stKeyBg.Render("(y/n)")
	} else if m.followMode {
		content = m.followBar()
	} else if m.ik != inNone {
		content = stKeyBg.Render("  "+promptLabel(m.ik)+" ") + m.input.View()
	} else if m.status != "" {
		content = padBetween(stBarBg.Render("  ")+m.keybar(m.width-len(m.status)-4), stKeyBg.Render(m.status+"  "), m.width)
	} else {
		content = stBarBg.Render("  ") + m.keybar(m.width-2)
	}
	// Fill the rest of the row with background-styled spaces. lipgloss won't
	// re-apply an outer background after the inner segments reset, so each cell
	// must carry the bar background itself.
	content += barPad(m.width - lipgloss.Width(content))
	return rule + "\n" + content
}

// followBar renders the follow-mode legend: each actionable token labeled with
// a letter to press (uppercase = regroup by it).
func (m *Model) followBar() string {
	sep := lipgloss.NewStyle().Foreground(cBorder).Background(cBarBg).Render("   ")
	out := stKeyBg.Render("  follow ") + stBarBg.Render(" ")
	for i, ft := range m.followTargets {
		if i > 0 {
			out += sep
		}
		var tok string
		switch ft.kind {
		case "link":
			tok = lipgloss.NewStyle().Foreground(cCyan).Background(cBarBg).Render("[[" + ft.value + "]]")
		case "tag":
			tok = lipgloss.NewStyle().Foreground(cMagenta).Background(cBarBg).Render("@" + ft.value)
		case "project":
			tok = lipgloss.NewStyle().Foreground(cBlue).Background(cBarBg).Render("+" + ft.value)
		}
		out += stKeyBg.Render(string(ft.label)) + stBarBg.Render(" ") + tok
	}
	out += sep + stBarBg.Render("(CAPS = group · esc)")
	return out
}

// chip renders a prominent badge (dark text on a bright background) for active
// view state — so a filter/scope can't silently shrink the list unnoticed.
func chip(label string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#16161e")).Background(bg).Bold(true).Render(" " + label + " ")
}

// barPad returns n background-styled spaces for filling a bar to full width.
func barPad(n int) string {
	if n < 1 {
		return ""
	}
	return stBarBg.Render(strings.Repeat(" ", n))
}

// keybarPairs returns the key hints relevant to the current tab and selection,
// most-important first — so the footer reflects what you can actually do here
// rather than a fixed menu (e.g. no due/tag on notes, "reopen" not "done" in the
// logbook, follow/link only when the selection has tokens to act on).
func (m *Model) keybarPairs() [][2]string {
	if m.locked {
		// Read-only: advertise only what still works, plus the unlock key.
		p := [][2]string{{"j/k", "move"}, {"enter", "detail"}, {"1/2/3", "tab"},
			{"/", "filter"}, {"y", "yank"}}
		if len(m.collectTargets()) > 0 {
			p = append(p, [2]string{"f", "follow"})
		}
		return append(p, [2]string{"^L", "unlock"})
	}
	hasTokens := len(m.collectTargets()) > 0
	switch m.tab {
	case tabNotes:
		p := [][2]string{{"j/k", "move"}, {"enter", "detail"}, {"A", "add note"}, {"r", "rename"}, {"e", "edit"}}
		if hasTokens {
			p = append(p, [2]string{"f", "follow"})
		}
		return append(p, [][2]string{{"/", "filter"}, {"1/2/3", "tab"}, {"u", "undo"}}...)
	case tabLogbook:
		p := [][2]string{{"j/k", "move"}, {"enter", "detail"}, {"x", "reopen"}, {"y", "yank"}}
		if hasTokens {
			p = append(p, [2]string{"f", "follow"})
		}
		return append(p, [][2]string{{"/", "search"}, {"1/2/3", "tab"}, {"u", "undo"}}...)
	default: // tasks
		p := [][2]string{{"j/k", "move"}, {"enter", "detail"}}
		if len(m.marked) > 0 {
			p = append(p, [][2]string{{"x", "done"}, {"X", "delete"}, {"p", "pri"},
				{"D", "due"}, {"t/T", "tag"}, {"esc", "clear marks"}}...)
		} else {
			p = append(p, [][2]string{{"x", "done"}, {"X", "delete"}, {"a/A", "add"},
				{"e", "edit"}, {"p", "pri"}, {"D", "due"}, {"t/T", "tag"}}...)
		}
		if hasTokens {
			p = append(p, [][2]string{{"l/L", "link"}, {"f", "follow"}}...)
		} else {
			p = append(p, [2]string{"l", "link"})
		}
		p = append(p, [][2]string{{"spc", "mark"}, {"y", "yank"}, {"/", "filter"}, {"v", "group"}, {"b", "blocked"}}...)
		if m.width >= wideMin {
			p = append(p, [2]string{"‹ ›", "width"})
		}
		return append(p, [2]string{"u", "undo"})
	}
}

// keybar renders key/label pairs (cyan key, dim label) in a fixed priority
// order. `? help` and `q quit` are always pinned at the end; when the remaining
// keys don't fit they are dropped from the low-priority end and a `…` is shown
// so it's clear more exist (under `?`) — the footer never silently loses verbs.
func (m *Model) keybar(width int) string {
	pairs := m.keybarPairs()
	sep := lipgloss.NewStyle().Foreground(cBorder).Background(cBarBg).Render(" · ")
	ell := lipgloss.NewStyle().Foreground(cDim).Background(cBarBg).Render("…")
	seg := func(p [2]string) string { return stKeyBg.Render(p[0]) + stBarBg.Render(" "+p[1]) }
	tail := seg([2]string{"?", "help"}) + sep + seg([2]string{"q", "quit"})

	tailW := lipgloss.Width(sep) + lipgloss.Width(tail) // " · ? help · q quit"
	ellW := lipgloss.Width(sep) + lipgloss.Width(ell)   // " · …"

	out := ""
	allFit := true
	for i, p := range pairs {
		s := seg(p)
		if out != "" {
			s = sep + s
		}
		// Keep room for the pinned tail, plus the "…" marker unless this is the
		// last pair (nothing left to hide after it).
		reserve := tailW
		if i < len(pairs)-1 {
			reserve += ellW
		}
		if lipgloss.Width(out)+lipgloss.Width(s)+reserve > width {
			allFit = false
			break
		}
		out += s
	}
	if !allFit {
		out += sep + ell // signal that more keys are available under ?
	}
	if out != "" {
		out += sep
	}
	return out + tail
}

// --- layouts -------------------------------------------------------------

func (m *Model) wideView(h int) string {
	leftW := m.splitWidth()
	rightW := m.width - leftW
	var listContent string
	switch m.tab {
	case tabNotes:
		listContent = m.notesList(leftW, h)
	case tabLogbook:
		listContent = m.logbookView(leftW, h)
	default:
		listContent = m.listView(leftW, h)
	}
	list := lipgloss.NewStyle().Width(leftW).Height(h).Render(listContent)
	// stPanel: 1 border + 2+2 padding = 5 cols of chrome.
	panel := stPanel
	if m.detailFocus {
		panel = stPanelFocus
	}
	content := m.scrollDetail(m.rightDetail(rightW-6), h)
	detail := panel.Width(rightW - 1).Height(h).Render(content)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
}

// scrollDetail windows the detail content to h rows at the current scroll
// offset (clamped), and appends a scroll indicator when more is below/above.
func (m *Model) scrollDetail(content string, h int) string {
	lines := strings.Split(content, "\n")
	n := len(lines)
	if n <= h || h <= 0 {
		m.detailScroll = 0
		return content
	}
	if m.detailScroll > n-h {
		m.detailScroll = n - h
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
	out := strings.Join(lines[m.detailScroll:m.detailScroll+h-1], "\n")
	more := n - h - m.detailScroll
	ind := fmt.Sprintf("↑%d ↓%d", m.detailScroll, max0(more))
	if !m.detailFocus {
		ind += "  (enter to scroll)"
	}
	return out + "\n" + stDim.Render(ind)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// rightDetail picks the detail pane content for the active tab.
func (m *Model) rightDetail(w int) string {
	if m.tab == tabNotes {
		if n := m.selectedNote(); n != nil {
			return m.noteDetail(n, w)
		}
		return stDim.Render("no note selected")
	}
	return m.detailContent(w)
}

func (m *Model) standardView(h int) string {
	switch m.tab {
	case tabNotes:
		return m.notesList(m.width, h)
	case tabLogbook:
		return m.logbookView(m.width, h)
	}
	return m.listView(m.width, h)
}

func (m *Model) compactView(h int) string {
	if m.tab == tabNotes {
		return m.notesList(m.width, h)
	}
	if m.tab == tabLogbook {
		return m.logbookView(m.width, h)
	}
	var lines []string
	var hits []hitLine
	idx, sel := 0, -1
	for _, g := range m.groups {
		for _, t := range g.tasks {
			cur := idx == m.cursor
			lines = append(lines, m.compactRow(t, cur))
			if cur {
				sel = len(lines) - 1
			}
			hits = append(hits, hitLine{item: idx})
			idx++
		}
	}
	m.hitLines = hits
	if len(lines) == 0 {
		return stDim.Render(" no tasks")
	}
	return m.viewport(lines, sel, h)
}

// compactRow renders one task for the narrow monitoring strip. The title is the
// row's identity, so it is protected: the priority column is dropped before the
// title is cut, inline @tag/+project/[[link]] tokens are shed before the title
// words are truncated, and only then is the title itself truncated.
func (m *Model) compactRow(t *task.Task, cur bool) string {
	if cur {
		// selRow draws a 3-col bar; glyph + space take 2 more.
		title, showMeta := fitTitleMeta(t.Text, plainMeta(t), m.width-5)
		body := glyphFor(m.effStatus(t)) + " " + title
		if showMeta {
			body += " " + plainMeta(t)
		}
		return selRow(body, m.width, m.marked[t.ID()])
	}
	pri := priStr(t.Priority)
	title, showMeta := fitTitleMeta(t.Text, pri, m.width-3) // gutter + glyph + space
	row := m.markGutter(t) + m.icon(t) + " " + colorizeStr(title, t.Done)
	if showMeta {
		row += " " + pri
	}
	return row
}

// fitTitleMeta fits a plain title plus an optional right-aligned meta column into
// budget columns. It returns the (possibly token-shed / truncated) title and
// whether the meta still fits beside it — meta is dropped before the title is cut.
func fitTitleMeta(text, meta string, budget int) (string, bool) {
	if budget < 1 {
		budget = 1
	}
	if meta != "" && lipgloss.Width(text)+lipgloss.Width(meta)+1 <= budget {
		return text, true // both fit
	}
	return compactTitle(text, budget), false // protect the title, drop the meta
}

// compactTitle fits a title into avail columns, preferring to shed inline tokens
// (@tag/+project/[[link]]) over cutting the title's words; truncates only if the
// core words alone still overflow.
func compactTitle(text string, avail int) string {
	if lipgloss.Width(text) <= avail {
		return text
	}
	if core := stripTokens(text); core != "" && core != text && lipgloss.Width(core) <= avail {
		return core
	}
	return truncate(text, avail)
}

// stripTokens removes inline @tag / +project / [[link]] words from task text,
// leaving the prose title.
func stripTokens(text string) string {
	fields := strings.Fields(text)
	out := fields[:0]
	for _, w := range fields {
		if strings.HasPrefix(w, "@") || strings.HasPrefix(w, "+") ||
			(strings.HasPrefix(w, "[[") && strings.HasSuffix(w, "]]")) {
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// rowRenderer draws one task line for a grouped list (sel = it's the cursor).
type rowRenderer func(t *task.Task, sel bool, width int) string

// renderGroupedList renders grouped tasks windowed to h rows: a header per
// group, each task via rowFn, building the per-line click map as it goes. Shared
// by the Tasks list and the Logbook so the loop, hit-testing, and empty-state
// handling live in exactly one place.
func (m *Model) renderGroupedList(groups []group, width, h int, rowFn rowRenderer, empty string) string {
	total := 0
	for _, g := range groups {
		total += len(g.tasks)
	}
	if total == 0 {
		m.hitLines = nil
		return stDim.Render(empty)
	}
	var lines []string
	var hits []hitLine
	idx, sel := 0, -1
	for gi, g := range groups {
		if gi > 0 {
			lines = append(lines, "")
			hits = append(hits, hitLine{item: -1})
		}
		lines = append(lines, groupHeader(g.name, len(g.tasks), width))
		hits = append(hits, hitLine{item: -1})
		for _, t := range g.tasks {
			cur := idx == m.cursor
			lines = append(lines, rowFn(t, cur, width))
			hits = append(hits, hitLine{item: idx, tokens: taskTokenSpans(t, 5)})
			if cur {
				sel = len(lines) - 1
			}
			idx++
		}
	}
	m.hitLines = hits
	return m.viewport(lines, sel, h)
}

// listView renders the grouped task list, windowed to h rows.
func (m *Model) listView(width, h int) string {
	empty := "  no tasks — press 'a' to add one"
	if m.filter != "" {
		empty = fmt.Sprintf("  no tasks match %q", m.filter)
	}
	return m.renderGroupedList(m.groups, width, h, m.taskRow, empty)
}

// logbookView renders the Logbook: completed tasks grouped by completion date,
// newest first, each row showing who the task came from (src).
func (m *Model) logbookView(width, h int) string {
	empty := "  no completed tasks yet — finish one with 'x'"
	if m.filter != "" {
		empty = fmt.Sprintf("  no completed tasks match %q", m.filter)
	}
	return m.renderGroupedList(m.logGroups, width, h, m.logRow, empty)
}

// logRow renders one Logbook line: green ✓, struck-through text, source on the
// right.
func (m *Model) logRow(t *task.Task, sel bool, width int) string {
	if sel {
		return selMetaRow("✓", t.Text, t.Source(), width, m.marked[t.ID()])
	}
	return listRow(m.markGutter(t), stGreen.Render("✓"), colorizeStr(t.Text, true), logSource(t), width)
}

// logSource styles a task's provenance for the Logbook (claude = magenta, any
// other source = cyan, none = blank).
func logSource(t *task.Task) string {
	s := t.Source()
	if s == "" {
		return ""
	}
	if s == "claude" {
		return stTag.Render(s)
	}
	return stLink.Render(s)
}

// groupHeader renders a labelled separator like "TODAY ──────────── (3)".
func groupHeader(name string, count, width int) string {
	label := "  " + strings.ToUpper(name) + " "
	tail := stDim.Render(fmt.Sprintf(" %d ", count))
	ruleLen := width - lipgloss.Width(label) - lipgloss.Width(tail) - 1
	if ruleLen < 1 {
		ruleLen = 1
	}
	return stGroup.Render(label) + stRule.Render(strings.Repeat("─", ruleLen)) + tail
}

// sectionHeader renders an underlined detail section label with a trailing rule.
func sectionHeader(label string, width int) string {
	l := stSec.Render(label) + " "
	ruleLen := width - lipgloss.Width(l) - 1
	if ruleLen < 0 {
		ruleLen = 0
	}
	return l + stRule.Render(strings.Repeat("─", ruleLen))
}

func (m *Model) notesList(width, h int) string {
	if len(m.notesView) == 0 {
		if m.filter != "" {
			return stDim.Render(fmt.Sprintf("  no notes match %q", m.filter))
		}
		return stDim.Render("  no notes — press 'A' to add one")
	}
	var lines []string
	var hits []hitLine
	for i, n := range m.notesView {
		folder := relFolder(n.Rel)
		if i == m.cursor {
			body := "▤ "
			if folder != "" {
				body += folder + "/"
			}
			body += n.Title
			if len(n.Tags) > 0 {
				body += "  @" + strings.Join(n.Tags, " @")
			}
			lines = append(lines, selRow(body, width, false))
		} else {
			row := stDim.Render("▤") + " "
			if folder != "" {
				row += stDim.Render(folder + "/")
			}
			row += n.Title
			if len(n.Tags) > 0 {
				row += "  " + stTag.Render("@"+strings.Join(n.Tags, " @"))
			}
			lines = append(lines, "   "+row)
		}
		hits = append(hits, hitLine{item: i})
	}
	m.hitLines = hits
	return m.viewport(lines, m.cursor, h)
}

// taskRow renders one task line sized to width, highlighting if selected.
func (m *Model) taskRow(t *task.Task, sel bool, width int) string {
	status := m.effStatus(t)
	if sel {
		return selMetaRow(glyphFor(status), t.Text, plainMeta(t), width, m.marked[t.ID()])
	}
	return listRow(m.markGutter(t), iconFor(status), colorizeStr(t.Text, t.Done), metaStr(t), width)
}

// selMetaRow lays out a selected "glyph text … meta" row on the solid selection
// bar. text and meta must be plain (selRow renders plain content); see selRow.
func selMetaRow(glyph, text, meta string, width int, marked bool) string {
	avail := width - 5 - lipgloss.Width(meta) // 3 bar + glyph + space
	if avail < 4 {
		avail = 4
	}
	left := glyph + " " + truncate(text, avail)
	gap := width - 3 - lipgloss.Width(left) - lipgloss.Width(meta)
	if gap < 1 {
		gap = 1
	}
	return selRow(left+strings.Repeat(" ", gap)+meta, width, marked)
}

// listRow lays out an unselected "gutter icon text … meta" row padded to width.
// text and meta are already styled by the caller.
func listRow(gutter, icon, text, meta string, width int) string {
	avail := width - 6 - lipgloss.Width(meta)
	if avail < 4 {
		avail = 4
	}
	if lipgloss.Width(text) > avail {
		text = truncate(text, avail)
	}
	left := gutter + "  " + icon + " " + text
	gap := width - lipgloss.Width(left) - lipgloss.Width(meta) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + meta + " "
}

// --- detail --------------------------------------------------------------

func (m *Model) detailCard(h int) string {
	t := m.selectedTask()
	n := m.selectedNote()
	w := m.width * 8 / 10
	if w > 70 {
		w = 70
	}
	var inner string
	switch {
	case m.tab == tabTasks && t != nil:
		inner = m.detailContent(w - 4)
	case m.tab == tabNotes && n != nil:
		inner = m.noteDetail(n, w-4)
	default:
		inner = stDim.Render("nothing selected")
	}
	cardH := h - 4 // leave a margin around the centered card
	if cardH < 4 {
		cardH = 4
	}
	inner = m.scrollDetail(inner, cardH)
	card := stCard.Width(w).Render(inner)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, card)
}

func (m *Model) detailContent(w int) string {
	t := m.selectedTask()
	if t == nil {
		return stDim.Render("no task selected")
	}
	var b strings.Builder
	b.WriteString(stTitle.Render(truncate(t.Text, w)) + "\n\n")
	kv := func(k, v string) {
		if v != "" {
			b.WriteString(stDim.Render(fmt.Sprintf("%-9s", k)) + v + "\n")
		}
	}
	kv("status", m.icon(t)+" "+m.effStatus(t))
	kv("priority", priStr(t.Priority))
	kv("due", dueStr(t.Due()))
	if p := t.Projects(); len(p) > 0 {
		kv("project", stProj.Render("+"+strings.Join(p, " +")))
	}
	if tg := t.Tags(); len(tg) > 0 {
		kv("tags", stTag.Render("@"+strings.Join(tg, " @")))
	}
	kv("source", t.Source())
	kv("id", stDim.Render(t.ID()))

	// Links + backlinks (SPEC §5.1).
	d, _ := m.eng.Read()
	m.renderForwardLinks(&b, d, t.Links(), w)
	m.renderBacklinks(&b, t.ID(), "", w)

	// Provenance: where this task came from, and what was discovered from it.
	if df := t.Discovered(); df != "" {
		if origin := d.FindByID(df); origin != nil {
			b.WriteString("\n" + sectionHeader("DISCOVERED FROM ↑", w) + "\n")
			b.WriteString("  " + stProj.Render("↑") + " " + truncate(origin.Text, w-4) + "\n")
		}
	}
	var spawned []*task.Task
	for _, o := range d.Tasks() {
		if o.Discovered() == t.ID() {
			spawned = append(spawned, o)
		}
	}
	if len(spawned) > 0 {
		b.WriteString("\n" + sectionHeader("DISCOVERED HERE ↳", w) + "\n")
		for _, o := range spawned {
			b.WriteString("  " + stTag.Render("↳") + " " + truncate(o.Text, w-4) + "\n")
		}
	}
	return b.String()
}

// renderForwardLinks appends a "LINKS →" section resolving each [[target]],
// showing the alias when present and flagging ambiguous / unresolved targets.
// Shared by the task and note detail panes.
func (m *Model) renderForwardLinks(b *strings.Builder, d *task.Doc, targets []string, w int) {
	if len(targets) == 0 {
		return
	}
	b.WriteString("\n" + sectionHeader("LINKS →", w) + "\n")
	for _, target := range targets {
		key, alias := links.NormalizeTarget(target)
		disp := key
		if alias != "" {
			disp = alias
		}
		switch it, ok := links.Resolve(target, d, m.notes); {
		case ok:
			b.WriteString("  " + stProj.Render("→") + " " + stDim.Render("["+it.Kind+"]") + " " + truncate(it.Title, w-10) + "\n")
		case it.Kind == "ambiguous":
			b.WriteString("  " + stWarn.Render("→") + " " + stWarn.Render("[["+disp+"]]") + stDim.Render(" (ambiguous)") + "\n")
		default:
			b.WriteString("  " + stWarn.Render("→") + " " + stWarn.Render("[["+disp+"]]") + stDim.Render(" (unresolved)") + "\n")
		}
	}
}

// renderBacklinks appends a "LINKED FROM ←" section for the item (id for tasks,
// id+rel for notes). Shared by both detail panes.
func (m *Model) renderBacklinks(b *strings.Builder, id, rel string, w int) {
	back := links.Backlinks(m.eng.S, id, rel)
	if len(back) == 0 {
		return
	}
	b.WriteString("\n" + sectionHeader("LINKED FROM ←", w) + "\n")
	for _, h := range back {
		kind, title := m.backlinkLabel(h.Path, h.Text)
		b.WriteString("  " + stTag.Render("←") + " " + stDim.Render("["+kind+"]") + " " + truncate(title, w-10) + "\n")
	}
}

// backlinkLabel turns a backlink hit (file path + matching line) into a human
// label — a note title or a task's text — instead of a raw file path.
func (m *Model) backlinkLabel(path, text string) (kind, title string) {
	if strings.HasSuffix(path, ".md") {
		for _, n := range m.notes {
			if n.Path == path {
				return "note", n.Title
			}
		}
		return "note", strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if tk, ok := task.ParseLine(strings.TrimSpace(text)); ok {
		return "task", tk.Text
	}
	return "task", strings.TrimSpace(text)
}

// relFolder returns a note's slash-separated parent folder (relative to notes/),
// or "" for a note in the root. Rel is "" for an unsaved/in-memory note.
func relFolder(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}

func (m *Model) noteDetail(n *note.Note, w int) string {
	var b strings.Builder
	b.WriteString(stTitle.Render(truncate(n.Title, w)) + "\n\n")
	if folder := relFolder(n.Rel); folder != "" {
		b.WriteString(stDim.Render("folder   ") + stMuted.Render(folder+"/") + "\n")
	}
	if len(n.Tags) > 0 {
		b.WriteString(stDim.Render("tags     ") + stTag.Render("@"+strings.Join(n.Tags, " @")) + "\n")
	}
	if len(n.Aliases) > 0 {
		b.WriteString(stDim.Render("aliases  ") + stMuted.Render(strings.Join(n.Aliases, ", ")) + "\n")
	}
	if n.Source != "" {
		b.WriteString(stDim.Render("source   ") + n.Source + "\n")
	}
	b.WriteString("\n" + sectionHeader("BODY", w) + "\n")
	b.WriteString(m.renderMarkdown(n.ID, n.Body, w))

	// Surface the note's outgoing links (resolved / aliased / ambiguous) and its
	// backlinks — the same treatment the task detail gets.
	d, _ := m.eng.Read()
	m.renderForwardLinks(&b, d, extractLinks(n.Body), w)
	m.renderBacklinks(&b, n.ID, n.Rel, w)
	return b.String()
}

// mdCache memoizes the last glamour render so View() doesn't re-render the
// markdown body on every frame.
type mdCache struct {
	id, body string
	w        int
	out      string
}

// renderMarkdown renders a note body to styled terminal markdown via glamour,
// cached by note id + width + body. Falls back to plain text on any error.
func (m *Model) renderMarkdown(id, body string, w int) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	if w < 8 {
		w = 8
	}
	// Render the full body — the result is cached (keyed by id/width/body) and
	// scrollDetail windows the output, so cost is paid once. The cap is only a
	// guard against a pathologically huge file freezing the one-time render.
	const maxBody = 1 << 20 // 1 MiB
	if len(body) > maxBody {
		body = body[:maxBody] + "\n\n… (truncated)"
	}
	if m.md.id == id && m.md.w == w && m.md.body == body {
		return m.md.out
	}
	out := stMuted.Render(body)
	if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(w)); err == nil {
		if s, err := r.Render(body); err == nil {
			out = strings.Trim(s, "\n")
		}
	}
	m.md = mdCache{id: id, body: body, w: w, out: out}
	return out
}

func (m *Model) helpView() string {
	groups := []struct {
		title string
		rows  [][2]string
	}{
		{"navigate", [][2]string{
			{"j / k, ↑ ↓", "move"}, {"Ctrl+d / Ctrl+u", "half-page down / up"},
			{"g / G", "top / bottom"}, {"1 / 2 / 3 / tab", "tasks / notes / logbook"},
			{"enter", "focus detail (then j/k scroll the body)"}, {"esc", "back to list"},
		}},
		{"select", [][2]string{
			{"space", "mark / unmark the current task"},
			{"V", "visual range-select (move to extend)"},
			{"y", "yank → y id · l line · t text (marks if any)"},
			{"esc", "clear marks → filter → scope"},
		}},
		{"edit (acts on marks if any, else current)", [][2]string{
			{"x  or  dd", "toggle done"}, {"X", "delete (confirms; u to undo)"},
			{"a / A", "add task / note"},
			{"r", "rename"}, {"e / E", "edit in $EDITOR"}, {"p", "cycle priority"},
			{"D", "set due date"}, {"t / T", "add / remove tag"},
			{"l / L", "add a [[link]] / jump to the first link"}, {"u", "undo (again = redo)"},
		}},
		{"view", [][2]string{
			{"f", "follow: pick any [[link]]/@tag/+project to open or scope (CAPS = group)"},
			{"mouse", "wheel scrolls · click selects · click a token activates it"},
			{"/", "filter (searches note bodies on the notes tab)"},
			{"esc", "clear filter / scope"},
			{"v", "cycle grouping (date→project→tag)"},
			{"‹ ›", "resize the list/detail split (or drag the divider)"},
			{"ctrl+l", "lock / unlock (read-only: blocks all writes)"},
			{".", "show / hide done"}, {"b", "show / hide blocked"},
			{"?", "this help"}, {"q", "back out one level, then quit"},
		}},
	}
	// Build every line first so we can window it to the terminal height.
	lines := []string{stTitle.Render("nt — keys")}
	for _, g := range groups {
		lines = append(lines, "", stSec.Render(strings.ToUpper(g.title)))
		for _, r := range g.rows {
			lines = append(lines, "  "+stKey.Render(fmt.Sprintf("%-16s", r[0]))+stMuted.Render(r[1]))
		}
	}
	// Keep the card width stable across scrolling (widest line, not just the window).
	cardW := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > cardW {
			cardW = w
		}
	}

	// Card chrome: border(2) + padding(2) + blank + footer + a row of outer margin.
	bodyH := m.height - 7
	if bodyH < 3 {
		bodyH = 3
	}
	var footer string
	if len(lines) > bodyH {
		maxScroll := len(lines) - bodyH
		if m.helpScroll > maxScroll {
			m.helpScroll = maxScroll
		}
		if m.helpScroll < 0 {
			m.helpScroll = 0
		}
		lines = lines[m.helpScroll : m.helpScroll+bodyH]
		footer = stDim.Render(fmt.Sprintf("↑%d ↓%d · j/k scroll · ? esc close", m.helpScroll, maxScroll-m.helpScroll))
	} else {
		m.helpScroll = 0
		footer = stDim.Render("press ? or esc to close")
	}

	body := strings.Join(lines, "\n") + "\n\n" + footer
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		stCard.Width(cardW).Render(body))
}

// --- small render helpers ------------------------------------------------

// icon renders a task's status glyph, accounting for dependency blocking.
func (m *Model) icon(t *task.Task) string { return iconFor(m.effStatus(t)) }

func iconFor(status string) string {
	switch status {
	case "done":
		return stDim.Render("✓")
	case "doing":
		return lipgloss.NewStyle().Foreground(cGreen).Render("◐")
	case "blocked":
		return lipgloss.NewStyle().Foreground(cRed).Render("⊘")
	default:
		return stDim.Render("○")
	}
}

func priStr(p byte) string {
	switch p {
	case 'A':
		return lipgloss.NewStyle().Foreground(cRed).Render("(A)")
	case 'B':
		return lipgloss.NewStyle().Foreground(cYellow).Render("(B)")
	case 'C':
		return lipgloss.NewStyle().Foreground(cBlue).Render("(C)")
	}
	return ""
}

// dueLabel returns the short relative label and its color (no styling applied).
func dueLabel(due string) (string, lipgloss.Color) {
	if due == "" {
		return "", cDim
	}
	dt, err := time.Parse("2006-01-02", due)
	if err != nil {
		return due, cDim
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	switch {
	case due < today:
		return due, cRed
	case due == today:
		return "today", cOrange
	case due == now.AddDate(0, 0, 1).Format("2006-01-02"):
		return "tom", cYellow
	case dt.Before(now.AddDate(0, 0, 7)):
		return dt.Weekday().String()[:3], cYellow
	}
	return due, cDim
}

func dueStr(due string) string {
	if due == "" {
		return ""
	}
	l, c := dueLabel(due)
	return lipgloss.NewStyle().Foreground(c).Render(l)
}

func metaStr(t *task.Task) string {
	parts := []string{}
	if p := priStr(t.Priority); p != "" {
		parts = append(parts, p)
	}
	if d := dueStr(t.Due()); d != "" {
		parts = append(parts, d)
	}
	return strings.Join(parts, " ")
}

// plainMeta is metaStr without ANSI, for selected rows (rendered on a solid bg).
func plainMeta(t *task.Task) string {
	parts := []string{}
	switch t.Priority {
	case 'A':
		parts = append(parts, "(A)")
	case 'B':
		parts = append(parts, "(B)")
	case 'C':
		parts = append(parts, "(C)")
	}
	if l, _ := dueLabel(t.Due()); l != "" {
		parts = append(parts, l)
	}
	return strings.Join(parts, " ")
}

// glyphFor is the plain status glyph (no color), for selected rows.
func glyphFor(status string) string {
	switch status {
	case "done":
		return "✓"
	case "doing":
		return "◐"
	case "blocked":
		return "⊘"
	default:
		return "○"
	}
}

// selRow renders a selected list row: a blue accent bar plus PLAIN body text on
// the selection background, filled to the full width. Keeping the body plain
// (no inner ANSI resets) lets lipgloss fill the background cleanly to the edge —
// the fix for the ragged selection bar and the bad-color-blend artifact.
func selRow(body string, width int, marked bool) string {
	mark := " "
	if marked {
		mark = "●"
	}
	bar := lipgloss.NewStyle().Foreground(cYellow).Background(cSelBg).Render(mark) +
		lipgloss.NewStyle().Foreground(cBlue).Background(cSelBg).Render("▌ ")
	bw := lipgloss.Width(bar)
	if lipgloss.Width(body) > width-bw {
		body = truncate(body, width-bw)
	}
	return bar + lipgloss.NewStyle().Background(cSelBg).Foreground(cFg).Width(width-bw).Render(body)
}

// markGutter is the 1-col left gutter glyph for a non-selected row.
func (m *Model) markGutter(t *task.Task) string {
	if m.marked[t.ID()] {
		return lipgloss.NewStyle().Foreground(cYellow).Render("●")
	}
	return " "
}

func colorizeStr(text string, done bool) string {
	if done {
		return stDone.Render(text)
	}
	words := strings.Fields(text)
	for i, w := range words {
		switch {
		case strings.HasPrefix(w, "@") && len(w) > 1:
			words[i] = stTag.Render(w)
		case strings.HasPrefix(w, "+") && len(w) > 1:
			words[i] = stProj.Render(w)
		case strings.HasPrefix(w, "[["):
			words[i] = stLinkU.Render(w) // underline: reads as a followable link
		}
	}
	return strings.Join(words, " ")
}

func promptLabel(ik inputKind) string {
	switch ik {
	case inAddTask:
		return "add:"
	case inAddNote:
		return "note:"
	case inFilter:
		return "filter:"
	case inRename:
		return "rename:"
	case inDue:
		return "due:"
	case inTag:
		return "tag:"
	case inUntag:
		return "untag:"
	case inSetPri:
		return "priority:"
	case inRenameNote:
		return "rename:"
	case inLink:
		return "link:"
	}
	return ">"
}

// padBetween left-justifies left and right-justifies right within width.
func padBetween(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// truncate cuts s to a max display width, adding an ellipsis. ANSI-aware via
// lipgloss width; for styled strings it trims conservatively by runes.
func truncate(s string, max int) string {
	if max < 1 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max-1 {
		r = r[:max-1]
	}
	return string(r) + "…"
}

// viewport returns up to h lines, scrolling only when the selected line nears an
// edge (a scrolloff margin). Unlike a recentering window, the cursor stays put
// in the middle band and the list feels planted. It keeps m.offset as the
// persistent scroll position.
func (m *Model) viewport(lines []string, sel, h int) string {
	n := len(lines)
	if n <= h || h <= 0 {
		m.offset = 0
		return strings.Join(lines, "\n")
	}
	so := 4 // scrolloff
	if h < 2*so+1 {
		so = (h - 1) / 2
	}
	if sel >= 0 {
		if sel < m.offset+so {
			m.offset = sel - so
		}
		if sel > m.offset+h-1-so {
			m.offset = sel - (h - 1 - so)
		}
	}
	if m.offset > n-h {
		m.offset = n - h
	}
	if m.offset < 0 {
		m.offset = 0
	}
	return strings.Join(lines[m.offset:m.offset+h], "\n")
}
