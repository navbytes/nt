package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/task"
)

func testModel(t *testing.T) *Model {
	t.Setenv("NT_DIR", t.TempDir())
	eng, err := mutate.Open()
	if err != nil {
		t.Fatal(err)
	}
	seed := []string{"fix auth bug @backend +api due:" + mutate.Today(), "write tests +api", "deploy"}
	for _, s := range seed {
		txt := s
		_ = eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
			nt := task.New(txt)
			d.Append(nt)
			rec.Added(nt)
			return nil
		})
	}
	m := &Model{eng: eng, input: textinput.New(), marked: map[string]bool{}}
	m.reload()
	m.ready = true // skip the startup key-gate in tests
	return m
}

// TestStartupKeyGate confirms keys are ignored until the ready signal, so a
// stray terminal-query byte at launch can't switch tabs.
func TestStartupKeyGate(t *testing.T) {
	m := testModel(t)
	m.ready = false
	m.width, m.height = 100, 24
	var model tea.Model = m
	// A stray "]" (next tab) before ready must be dropped (stay on tasks).
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if model.(*Model).tab != tabTasks {
		t.Fatal("key before ready should be ignored")
	}
	// After ready, "]" switches to the next (notes) tab.
	model, _ = model.Update(readyMsg{})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if model.(*Model).tab != tabNotes {
		t.Fatal("key after ready should be handled")
	}
}

// TestViewRendersAllLayouts ensures every responsive layout, tab, and overlay
// renders without panicking and produces output.
func TestViewRendersAllLayouts(t *testing.T) {
	m := testModel(t)
	for _, w := range []int{30, 50, 80, 140} {
		m.width, m.height = w, 24
		for _, tb := range []tab{tabTasks, tabNotes} {
			m.tab = tb
			if out := m.View(); strings.TrimSpace(out) == "" {
				t.Fatalf("empty view at width=%d tab=%d", w, tb)
			}
		}
	}
	m.tab, m.width, m.detailFocus = tabTasks, 80, true
	if m.View() == "" {
		t.Fatal("empty detail overlay")
	}
	m.detailFocus, m.help = false, true
	if m.View() == "" {
		t.Fatal("empty help view")
	}
}

// TestKeyActionsNoPanic drives a sequence of keys through Update.
func TestKeyActionsNoPanic(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	keys := []string{"j", "j", "k", "v", "v", "G", "g", "p", "2", "1", ".", "?", "?"}
	var model tea.Model = m
	for _, k := range keys {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	// dd toggles done on the selected task.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := model.(*Model)
	done := 0
	for _, tk := range mm.tasks {
		if tk.Done {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("dd should have completed exactly one task, got %d", done)
	}
	// undo reopens it.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	mm = model.(*Model)
	for _, tk := range mm.tasks {
		if tk.Done {
			t.Fatal("undo should have reopened the task")
		}
	}
}

// TestAddViaInput exercises the add-task prompt end to end.
func TestAddViaInput(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 24
	before := len(m.tasks)
	var model tea.Model = m
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // open prompt
	for _, r := range "new thing" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := model.(*Model)
	if len(mm.tasks) != before+1 {
		t.Fatalf("want %d tasks after add, got %d", before+1, len(mm.tasks))
	}
}

func TestAdaptivePaletteThemes(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer lipgloss.SetHasDarkBackground(true) // restore default for other tests

	lipgloss.SetHasDarkBackground(true)
	dark := lipgloss.NewStyle().Foreground(cFg).Render("x")
	lipgloss.SetHasDarkBackground(false)
	light := lipgloss.NewStyle().Foreground(cFg).Render("x")
	if dark == light {
		t.Fatal("adaptive palette should render differently on light vs dark backgrounds")
	}
}
