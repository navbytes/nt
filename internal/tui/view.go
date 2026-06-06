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
	t1, t2 := " 1 tasks ", " 2 notes "
	if m.tab == tabTasks {
		t1, t2 = stTabOn.Render(t1), stTabOff.Render(t2)
	} else {
		t1, t2 = stTabOff.Render(t1), stTabOn.Render(t2)
	}
	left := stHeader.Render("  ") + stBrand.Render(" nt ") + stHeader.Render("  ") + t1 + t2

	// Persistently surface the view state (toggles + filter) in the header so
	// the user always knows what they're looking at.
	parts := []string{"group:" + m.grp.String()}
	if len(m.marked) > 0 {
		c := fmt.Sprintf("● %d marked", len(m.marked))
		if h := m.hiddenMarked(); h > 0 {
			c += fmt.Sprintf(" (%d hidden)", h)
		}
		parts = append(parts, c)
	}
	if m.scopeTag != "" {
		parts = append(parts, "@"+m.scopeTag)
	}
	if m.scopeProject != "" {
		parts = append(parts, "+"+m.scopeProject)
	}
	if m.showDone {
		parts = append(parts, "+done")
	}
	if m.showBlocked {
		parts = append(parts, "+blocked")
	}
	if m.filter != "" {
		parts = append(parts, fmt.Sprintf("/%s (%d)", m.filter, m.selectableLen()))
	}
	rightR := stBarBg.Render(strings.Join(parts, "  ·  ") + "  ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(rightR)
	line := left + barPad(gap) + rightR
	rule := stRule.Render(strings.Repeat("─", m.width))
	return line + "\n" + rule
}

func (m *Model) footerView() string {
	rule := stRule.Render(strings.Repeat("─", m.width))
	var content string
	if m.confirm != nil {
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

// barPad returns n background-styled spaces for filling a bar to full width.
func barPad(n int) string {
	if n < 1 {
		return ""
	}
	return stBarBg.Render(strings.Repeat(" ", n))
}

// keybar renders key/label pairs (cyan key, dim label) up to a width budget.
// `? help` and `q quit` are always reserved at the end so the discoverability
// keys never get truncated off-screen, however narrow the terminal.
func (m *Model) keybar(width int) string {
	pairs := [][2]string{
		{"j/k", "move"}, {"enter", "detail"}, {"x", "done"}, {"a/A", "add"},
		{"e", "edit"}, {"p", "pri"}, {"D", "due"}, {"t/T", "tag"}, {"l/L", "link"},
		{"spc", "mark"}, {"f", "follow"}, {"/", "filter"}, {"v", "group"}, {"b", "blocked"}, {"u", "undo"},
	}
	sep := lipgloss.NewStyle().Foreground(cBorder).Background(cBarBg).Render(" · ")
	seg := func(p [2]string) string { return stKeyBg.Render(p[0]) + stBarBg.Render(" "+p[1]) }
	tail := seg([2]string{"?", "help"}) + sep + seg([2]string{"q", "quit"})
	budget := width - lipgloss.Width(tail) - lipgloss.Width(sep)

	out := ""
	for _, p := range pairs {
		s := seg(p)
		if out != "" {
			s = sep + s
		}
		if lipgloss.Width(out)+lipgloss.Width(s) > budget {
			break
		}
		out += s
	}
	if out != "" {
		out += sep
	}
	return out + tail
}

// --- layouts -------------------------------------------------------------

func (m *Model) wideView(h int) string {
	leftW := m.width * 58 / 100
	rightW := m.width - leftW
	var listContent string
	if m.tab == tabNotes {
		listContent = m.notesList(leftW, h)
	} else {
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
	if m.tab == tabNotes {
		return m.notesList(m.width, h)
	}
	return m.listView(m.width, h)
}

func (m *Model) compactView(h int) string {
	if m.tab == tabNotes {
		return m.notesList(m.width, h)
	}
	var lines []string
	var hits []hitLine
	idx, sel := 0, -1
	for _, g := range m.groups {
		for _, t := range g.tasks {
			cur := idx == m.cursor
			if cur {
				body := glyphFor(m.effStatus(t)) + " " + truncate(t.Text, m.width-8)
				if p := plainMeta(t); p != "" {
					body += " " + p
				}
				lines = append(lines, selRow(body, m.width, m.marked[t.ID()]))
				sel = len(lines) - 1
			} else {
				row := m.icon(t) + " " + truncate(colorizeStr(t.Text, t.Done), m.width-6) + " " + priStr(t.Priority)
				lines = append(lines, m.markGutter(t)+row)
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

// listView renders the grouped task list, windowed to h rows.
func (m *Model) listView(width, h int) string {
	if len(m.flat) == 0 {
		m.hitLines = nil
		if m.filter != "" {
			return stDim.Render(fmt.Sprintf("  no tasks match %q", m.filter))
		}
		return stDim.Render("  no tasks — press 'a' to add one")
	}
	var lines []string
	var hits []hitLine
	idx, sel := 0, -1
	for gi, g := range m.groups {
		if gi > 0 {
			lines = append(lines, "")
			hits = append(hits, hitLine{item: -1})
		}
		lines = append(lines, groupHeader(g.name, len(g.tasks), width))
		hits = append(hits, hitLine{item: -1})
		for _, t := range g.tasks {
			cur := idx == m.cursor
			lines = append(lines, m.taskRow(t, cur, width))
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
		if i == m.cursor {
			body := "▤ " + n.Title
			if len(n.Tags) > 0 {
				body += "  @" + strings.Join(n.Tags, " @")
			}
			lines = append(lines, selRow(body, width, false))
		} else {
			row := stDim.Render("▤") + " " + n.Title
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
		// Plain content on a solid selection bar (see selRow).
		meta := plainMeta(t)
		avail := width - 5 - lipgloss.Width(meta) // 3 bar + glyph + space
		if avail < 4 {
			avail = 4
		}
		text := truncate(t.Text, avail)
		left := glyphFor(status) + " " + text
		gap := width - 3 - lipgloss.Width(left) - lipgloss.Width(meta)
		if gap < 1 {
			gap = 1
		}
		return selRow(left+strings.Repeat(" ", gap)+meta, width, m.marked[t.ID()])
	}
	icon := iconFor(status)
	meta := metaStr(t)
	avail := width - 6 - lipgloss.Width(meta)
	if avail < 4 {
		avail = 4
	}
	text := colorizeStr(t.Text, t.Done)
	if lipgloss.Width(text) > avail {
		text = truncate(text, avail)
	}
	left := m.markGutter(t) + "  " + icon + " " + text
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
	if fwd := t.Links(); len(fwd) > 0 {
		b.WriteString("\n" + sectionHeader("LINKS →", w) + "\n")
		for _, target := range fwd {
			if it, ok := links.Resolve(target, d, m.notes); ok {
				b.WriteString("  " + stProj.Render("→") + " " + stDim.Render("["+it.Kind+"]") + " " + truncate(it.Title, w-10) + "\n")
			} else {
				b.WriteString("  " + stProj.Render("→") + " " + stLink.Render("[["+target+"]]") + stDim.Render(" (unresolved)") + "\n")
			}
		}
	}
	back := links.Backlinks(m.eng.S, t.ID(), "")
	if len(back) > 0 {
		b.WriteString("\n" + sectionHeader("LINKED FROM ←", w) + "\n")
		for _, h := range back {
			kind, title := m.backlinkLabel(h.Path, h.Text)
			b.WriteString("  " + stTag.Render("←") + " " + stDim.Render("["+kind+"]") + " " + truncate(title, w-10) + "\n")
		}
	}
	return b.String()
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

func (m *Model) noteDetail(n *note.Note, w int) string {
	var b strings.Builder
	b.WriteString(stTitle.Render(truncate(n.Title, w)) + "\n\n")
	if len(n.Tags) > 0 {
		b.WriteString(stDim.Render("tags     ") + stTag.Render("@"+strings.Join(n.Tags, " @")) + "\n")
	}
	if n.Source != "" {
		b.WriteString(stDim.Render("source   ") + n.Source + "\n")
	}
	b.WriteString("\n" + sectionHeader("BODY", w) + "\n")
	b.WriteString(m.renderMarkdown(n.ID, n.Body, w))
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
	if len(body) > 4000 {
		body = body[:4000] + "\n\n…"
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
			{"g / G", "top / bottom"}, {"1 / 2 / tab", "switch tasks / notes"},
			{"enter", "focus detail (then j/k scroll the body)"}, {"esc", "back to list"},
		}},
		{"select", [][2]string{
			{"space", "mark / unmark the current task"},
			{"V", "visual range-select (move to extend)"},
			{"esc", "clear marks → filter → scope"},
		}},
		{"edit (acts on marks if any, else current)", [][2]string{
			{"x  or  dd", "toggle done"}, {"a / A", "add task / note"},
			{"r", "rename"}, {"e / E", "edit in $EDITOR"}, {"p", "cycle priority"},
			{"D", "set due date"}, {"t / T", "add / remove tag"},
			{"l / L", "add link / follow link"}, {"u", "undo (again = redo)"},
		}},
		{"view", [][2]string{
			{"f", "follow: label a [[link]]/@tag/+project to activate (CAPS = group)"},
			{"mouse", "wheel scrolls · click selects · click a token activates it"},
			{"/", "filter (searches note bodies on the notes tab)"},
			{"esc", "clear filter / scope"},
			{"v", "cycle grouping (date→project→tag)"},
			{".", "show / hide done"}, {"b", "show / hide blocked"},
			{"?", "this help"}, {"q", "quit"},
		}},
	}
	var b strings.Builder
	b.WriteString(stTitle.Render("nt — keys") + "\n")
	for _, g := range groups {
		b.WriteString("\n" + stSec.Render(strings.ToUpper(g.title)) + "\n")
		for _, r := range g.rows {
			b.WriteString("  " + stKey.Render(fmt.Sprintf("%-16s", r[0])) + stMuted.Render(r[1]) + "\n")
		}
	}
	b.WriteString("\n" + stDim.Render("press ? or esc to close"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		stCard.Render(b.String()))
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
			words[i] = stLink.Render(w)
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
