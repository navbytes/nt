// Package tui implements nt's terminal UI (SPEC §12) with Bubble Tea. The TUI
// holds its task/note lists as a read-only view (SPEC §6.1): every mutation is
// a ULID-keyed op applied through the shared mutate.Engine, after which the
// view reloads from disk. An fsnotify directory watch reloads the view when any
// other process (a CLI call, an AI session) changes the store.
package tui

import (
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
	inLink
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

	tasks   []*task.Task
	notes   []*note.Note
	blocked map[string]bool // task ULIDs blocked by an open dependency

	groups []group      // tasks tab, current grouping
	flat   []*task.Task // selectable tasks in display order
	cursor int          // index into flat (tasks) or notes (notes tab)

	filter string
	detail bool // detail overlay open (narrow modes)
	help   bool
	ready  bool // gates key input until startup terminal-query noise settles

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
	m := &Model{eng: eng, input: ti}
	m.reload()

	ch, stop, err := watchStore(eng.S.Dir)
	if err == nil {
		m.changes = ch
		defer stop()
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
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
// filter, grouping, and showDone state, then clamps the cursor.
func (m *Model) rebuild() {
	m.blocked = task.BlockedIDs(m.tasks)
	m.groups = buildGroups(m.tasks, m.grp, m.filter, m.showDone, m.showBlocked, m.blocked)
	m.flat = m.flat[:0]
	for _, g := range m.groups {
		m.flat = append(m.flat, g.tasks...)
	}
	m.clampCursor()
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
		return len(m.notes)
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
	if m.tab != tabNotes || m.cursor < 0 || m.cursor >= len(m.notes) {
		return nil
	}
	return m.notes[m.cursor]
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

	stBrand  = lipgloss.NewStyle().Foreground(cMagenta).Bold(true).Background(cBarBg)
	stTabOn  = lipgloss.NewStyle().Foreground(cFg).Bold(true).Background(cBarBg)
	stTabOff = lipgloss.NewStyle().Foreground(cDim).Background(cBarBg)
	stBar    = lipgloss.NewStyle().Foreground(cMuted)
	stBarBg  = lipgloss.NewStyle().Foreground(cMuted).Background(cBarBg)
	stHeader = lipgloss.NewStyle().Background(cBarBg)
	stRule   = lipgloss.NewStyle().Foreground(cBorder)
	stGroup  = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	stSel    = lipgloss.NewStyle().Background(cSelBg)
	stSelTxt = lipgloss.NewStyle().Background(cSelBg).Foreground(cFg).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stMuted  = lipgloss.NewStyle().Foreground(cMuted)
	stKey    = lipgloss.NewStyle().Foreground(cCyan)
	stKeyBg  = lipgloss.NewStyle().Foreground(cCyan).Background(cBarBg)
	stSec    = lipgloss.NewStyle().Foreground(cBlue).Bold(true)
	stTag    = lipgloss.NewStyle().Foreground(cMagenta)
	stProj   = lipgloss.NewStyle().Foreground(cBlue)
	stLink   = lipgloss.NewStyle().Foreground(cCyan)
	stDone   = lipgloss.NewStyle().Foreground(cDim).Strikethrough(true)
	stPanel  = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(cBorder).Padding(0, 2)
	stCard   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBlue).Padding(1, 2)
	stTitle  = lipgloss.NewStyle().Foreground(cFg).Bold(true)
)
