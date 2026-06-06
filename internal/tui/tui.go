// Package tui implements nt's terminal UI (SPEC §12) with Bubble Tea. The TUI
// holds its task/note lists as a read-only view (SPEC §6.1): every mutation is
// a ULID-keyed op applied through the shared mutate.Engine, after which the
// view reloads from disk. An fsnotify directory watch reloads the view when any
// other process (a CLI call, an AI session) changes the store.
package tui

import (
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// Layout breakpoints (terminal columns), per SPEC §12.
const (
	wideMin    = 120 // split list + detail
	compactMax = 55  // monitoring strip
)

type tab int

const (
	tabTasks tab = iota
	tabNotes
)

type groupMode int

const (
	groupDate groupMode = iota
	groupProject
	groupTag
)

func (g groupMode) String() string {
	switch g {
	case groupProject:
		return "project"
	case groupTag:
		return "tag"
	default:
		return "date"
	}
}

// inputKind identifies what the text-input prompt is collecting.
type inputKind int

const (
	inNone inputKind = iota
	inAddTask
	inAddNote
	inFilter
	inRename
	inDue
	inTag
	inUntag
	inLink
	inSetPri
)

// group is a labelled bucket of tasks in display order.
type group struct {
	name  string
	tasks []*task.Task
}

// Model is the Bubble Tea model.
type Model struct {
	eng *mutate.Engine

	width, height int
	tab           tab
	grp           groupMode
	showDone      bool
	showBlocked   bool

	tasks     []*task.Task
	notes     []*note.Note
	notesView []*note.Note    // notes after the active filter
	blocked   map[string]bool // task ULIDs blocked by an open dependency

	groups   []group      // tasks tab, current grouping
	flat     []*task.Task // selectable tasks in display order
	cursor   int          // index into flat (tasks) or notes (notes tab)
	offset   int          // first visible line (scroll position)
	hitLines []hitLine    // per-line click map from the last list render (mouse)

	filter       string
	scopeTag     string // active @tag scope (filters the list); "" = none
	scopeProject string // active +project scope; "" = none
	detailFocus  bool   // detail pane is focused: j/k scroll the body, not the list
	detailScroll int    // scroll offset within the focused detail pane
	help         bool
	ready        bool // gates key input until startup terminal-query noise settles

	followMode    bool           // hint mode: tokens are labeled for keyboard activation
	followTargets []followTarget // labeled actionable tokens (links/tags/projects)

	marked       map[string]bool // multi-select: marked task ULIDs (survives reload/regroup)
	visualMode   bool            // V range-select in progress
	visualAnchor int             // cursor index where V started
	confirm      *confirmState   // pending y/n confirmation for a destructive action

	input  textinput.Model
	ik     inputKind
	pendD  bool   // first 'd' of a 'dd'
	status string // transient status line

	editTmp string // temp file for an in-progress external task edit
	editID  string // ULID of the task being edited externally

	md mdCache // last glamour-rendered note body

	changes <-chan struct{} // fsnotify notifications
	quitErr error
}

// Run launches the TUI against the global store.
func Run() error {
	eng, err := mutate.Open()
	if err != nil {
		return err
	}
	ti := textinput.New()
	ti.Prompt = ""
	m := &Model{eng: eng, input: ti, marked: map[string]bool{}}
	m.reload()

	ch, stop, err := watchStore(eng.S.Dir)
	if err == nil {
		m.changes = ch
		defer stop()
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if os.Getenv("NT_MOUSE") != "0" {
		// Mouse: wheel scroll + click-to-select/activate. Hold Shift to bypass
		// for native text selection. Disable entirely with NT_MOUSE=0.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	_, err = p.Run()
	if err != nil {
		return err
	}
	return m.quitErr
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(waitForChange(m.changes), readyAfter)
}

// readyAfter fires once shortly after startup. Until then, key input is dropped
// so a terminal's responses to Bubble Tea's initial queries (device attributes,
// background color, cursor position) can't be misread as keystrokes — the cause
// of the occasional "launches on the notes tab" glitch.
func readyAfter() tea.Msg {
	time.Sleep(180 * time.Millisecond)
	return readyMsg{}
}

type readyMsg struct{}

// reload re-reads tasks and notes from disk and rebuilds the grouped view.
func (m *Model) reload() {
	if d, err := m.eng.Read(); err == nil {
		m.tasks = d.Tasks()
	}
	m.notes, _ = note.List(m.eng.S)
	m.rebuild()
}

// rebuild recomputes groups + the flat selectable list from the current tasks,
// filter, grouping, and showDone state. It preserves the selected task across
// the rebuild (so regrouping/filtering keeps the cursor on the same item rather
// than the same numeric index), then clamps.
func (m *Model) rebuild() {
	selID := m.selectedID()

	m.blocked = task.BlockedIDs(m.tasks)
	m.groups = buildGroups(m.scopedTasks(), m.grp, m.filter, m.showDone, m.showBlocked, m.blocked)
	m.flat = m.flat[:0]
	for _, g := range m.groups {
		m.flat = append(m.flat, g.tasks...)
	}

	needle := strings.ToLower(strings.TrimSpace(m.filter))
	m.notesView = m.notesView[:0]
	for _, n := range m.notes {
		if m.scopeTag != "" && !contains(n.Tags, m.scopeTag) {
			continue
		}
		if noteMatches(n, needle) {
			m.notesView = append(m.notesView, n)
		}
	}

	if selID != "" {
		m.selectByID(selID)
	}
	m.clampCursor()
}

// selectedID returns the ULID of the currently-selected item in the active tab.
func (m *Model) selectedID() string {
	if m.tab == tabNotes {
		if m.cursor >= 0 && m.cursor < len(m.notesView) {
			return m.notesView[m.cursor].ID
		}
		return ""
	}
	if m.cursor >= 0 && m.cursor < len(m.flat) {
		return m.flat[m.cursor].ID()
	}
	return ""
}

// selectByID moves the cursor to the item with the given ULID in the active tab.
func (m *Model) selectByID(id string) {
	if m.tab == tabNotes {
		for i, n := range m.notesView {
			if n.ID == id {
				m.cursor = i
				return
			}
		}
		return
	}
	for i, t := range m.flat {
		if t.ID() == id {
			m.cursor = i
			return
		}
	}
}

// halfPage is half the visible body height, for Ctrl+d / Ctrl+u scrolling.
func (m *Model) halfPage() int {
	h := (m.height - 4) / 2
	if h < 1 {
		h = 1
	}
	return h
}

// scopedTasks applies the active @tag / +project scope to the task list, before
// grouping/filtering. An empty scope returns all tasks.
func (m *Model) scopedTasks() []*task.Task {
	if m.scopeTag == "" && m.scopeProject == "" {
		return m.tasks
	}
	out := make([]*task.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if m.scopeTag != "" && !contains(t.Tags(), m.scopeTag) {
			continue
		}
		if m.scopeProject != "" && !contains(t.Projects(), m.scopeProject) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// noteMatches reports whether a note matches the filter across title, tags,
// body, and source — so the notes tab filter is a full-text search.
func noteMatches(n *note.Note, needle string) bool {
	if needle == "" {
		return true
	}
	hay := strings.ToLower(n.Title + " " + strings.Join(n.Tags, " ") + " " + n.Body + " " + n.Source)
	return strings.Contains(hay, needle)
}

func (m *Model) clampCursor() {
	n := m.selectableLen()
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) selectableLen() int {
	if m.tab == tabNotes {
		return len(m.notesView)
	}
	return len(m.flat)
}

func (m *Model) selectedTask() *task.Task {
	if m.tab != tabTasks || m.cursor < 0 || m.cursor >= len(m.flat) {
		return nil
	}
	return m.flat[m.cursor]
}

func (m *Model) selectedNote() *note.Note {
	if m.tab != tabNotes || m.cursor < 0 || m.cursor >= len(m.notesView) {
		return nil
	}
	return m.notesView[m.cursor]
}

func (m *Model) setStatus(s string) { m.status = s }

// effStatus / icon account for dependency blocking when displaying a task.
func (m *Model) effStatus(t *task.Task) string {
	return task.EffectiveStatus(t, m.blocked[t.ID()] && !t.Done)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// --- palette / styles (mirrors docs/tui-mockup.html) ---------------------

var (
	cFg      = lipgloss.Color("#c0caf5")
	cDim     = lipgloss.Color("#565f89")
	cMuted   = lipgloss.Color("#787c99")
	cRed     = lipgloss.Color("#f7768e")
	cOrange  = lipgloss.Color("#ff9e64")
	cYellow  = lipgloss.Color("#e0af68")
	cGreen   = lipgloss.Color("#9ece6a")
	cCyan    = lipgloss.Color("#7dcfff")
	cBlue    = lipgloss.Color("#7aa2f7")
	cMagenta = lipgloss.Color("#bb9af7")
	cBorder  = lipgloss.Color("#3b4261")
	cSelBg   = lipgloss.Color("#283457")
	cBarBg   = lipgloss.Color("#1f2335")

	stBrand      = lipgloss.NewStyle().Foreground(cMagenta).Bold(true).Background(cBarBg)
	stTabOn      = lipgloss.NewStyle().Foreground(cFg).Bold(true).Background(cBarBg)
	stTabOff     = lipgloss.NewStyle().Foreground(cDim).Background(cBarBg)
	stBar        = lipgloss.NewStyle().Foreground(cMuted)
	stBarBg      = lipgloss.NewStyle().Foreground(cMuted).Background(cBarBg)
	stHeader     = lipgloss.NewStyle().Background(cBarBg)
	stRule       = lipgloss.NewStyle().Foreground(cBorder)
	stGroup      = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	stSel        = lipgloss.NewStyle().Background(cSelBg)
	stSelTxt     = lipgloss.NewStyle().Background(cSelBg).Foreground(cFg).Bold(true)
	stDim        = lipgloss.NewStyle().Foreground(cDim)
	stMuted      = lipgloss.NewStyle().Foreground(cMuted)
	stKey        = lipgloss.NewStyle().Foreground(cCyan)
	stKeyBg      = lipgloss.NewStyle().Foreground(cCyan).Background(cBarBg)
	stSec        = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	stTag        = lipgloss.NewStyle().Foreground(cMagenta)
	stProj       = lipgloss.NewStyle().Foreground(cBlue)
	stLink       = lipgloss.NewStyle().Foreground(cCyan)
	stDone       = lipgloss.NewStyle().Foreground(cDim).Strikethrough(true)
	stPanel      = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(cBorder).Padding(0, 2)
	stPanelFocus = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(cBlue).Padding(0, 2)
	stCard       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBlue).Padding(1, 2)
	stTitle      = lipgloss.NewStyle().Foreground(cFg).Bold(true)
)
