package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/navbytes/nt/internal/mutate"
	"github.com/navbytes/nt/internal/note"
	"github.com/navbytes/nt/internal/task"
)

// TestRenderHTML renders real TUI frames to docs/tui-render.html for visual
// review against the mockup. It is skipped unless NT_RENDER_HTML=1, so it never
// runs in the normal suite.
//
//	NT_RENDER_HTML=1 go test ./internal/tui/ -run TestRenderHTML
func TestRenderHTML(t *testing.T) {
	if os.Getenv("NT_RENDER_HTML") == "" {
		t.Skip("set NT_RENDER_HTML=1 to render")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)

	t.Setenv("NT_DIR", t.TempDir())
	eng, err := mutate.Open()
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	fri, _ := time.Parse("2006-01-02", today)
	for fri.Weekday() != time.Friday {
		fri = fri.AddDate(0, 0, 1)
	}
	friday := fri.Format("2006-01-02")

	add := func(text string, kv map[string]string, pri byte) string {
		var id string
		_ = eng.Apply("add", func(d *task.Doc, rec *mutate.Recorder) error {
			tk := task.New(text)
			if pri != 0 {
				tk.SetPriority(pri)
			}
			for k, v := range kv {
				tk.SetKey(k, v)
			}
			d.Append(tk)
			rec.Added(tk)
			id = tk.ID()
			return nil
		})
		return id
	}
	add("fix auth bug @backend +api [[jwt-expiry]]", map[string]string{"due": today, "src": "claude", "s": "doing"}, 'A')
	add("deploy API v2 +api", map[string]string{"due": today}, 'A')
	cfg := add("update config +api", nil, 0)
	add("write migration +api", map[string]string{"due": friday, "blocks": cfg}, 'B')
	add("spike: rotate auth secrets [[jwt-expiry]]", nil, 'C')
	doneID := add("ship release notes", nil, 0)
	_ = eng.Apply("done", func(d *task.Doc, rec *mutate.Recorder) error {
		tk := d.FindByID(doneID)
		rec.Before(tk)
		tk.SetDone(true, today)
		return nil
	})
	_, _ = note.Create(eng.S, "JWT expiry", "Tokens last 24h; refresh after 7d.", []string{"auth", "backend"}, "claude")

	m := &Model{eng: eng, input: textinput.New()}
	m.showBlocked = true
	m.showDone = true
	m.reload()

	type frame struct{ title, html string }
	render := func(w, h int, setup func()) string {
		m.width, m.height = w, h
		if setup != nil {
			setup()
		}
		return ansiToHTML(m.View())
	}
	frames := []frame{
		{"Wide split (140 cols)", render(140, 28, func() { m.tab, m.detail = tabTasks, false })},
		{"Standard (80 cols)", render(80, 24, func() { m.tab, m.detail = tabTasks, false })},
		{"Detail overlay (80 cols)", render(80, 24, func() { m.detail = true })},
		{"Compact strip (34 cols)", render(34, 20, func() { m.detail = false })},
		{"Notes tab (80 cols)", render(80, 24, func() { m.tab, m.cursor = tabNotes, 0 })},
		{"Notes split (140 cols)", render(140, 28, func() { m.tab, m.cursor = tabNotes, 0 })},
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>nt TUI — live render</title>
<style>body{background:#0d0e14;color:#c0caf5;font-family:ui-monospace,Menlo,monospace;padding:28px}
h1{color:#fff}h2{color:#7aa2f7;font-size:13px;text-transform:uppercase;letter-spacing:.1em;margin:28px 0 8px}
.term{display:inline-block;border:1px solid #3b4261;border-radius:10px;overflow:hidden;box-shadow:0 10px 40px rgba(0,0,0,.5)}
.chrome{background:#11121a;border-bottom:1px solid #3b4261;padding:8px 12px}
.dot{display:inline-block;width:11px;height:11px;border-radius:50%;margin-right:6px}
pre{margin:0;padding:0;background:#1a1b26;line-height:1.32;font-size:13px}</style>
<h1>nt — live TUI render</h1>
<p style="color:#787c99">Actual View() output (truecolor → HTML). Compare with tui-mockup.html.</p>`)
	for _, f := range frames {
		b.WriteString("<h2>" + f.title + "</h2>\n")
		b.WriteString(`<div class="term"><div class="chrome"><span class="dot" style="background:#ff5f57"></span><span class="dot" style="background:#febc2e"></span><span class="dot" style="background:#28c840"></span></div>`)
		b.WriteString("<pre>" + f.html + "</pre></div>\n")
	}

	out := "../../docs/tui-render.html"
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", out)
}

// ansiToHTML converts truecolor ANSI SGR output into styled HTML spans.
func ansiToHTML(s string) string {
	var b strings.Builder
	var fg, bg string
	var bold, ul, strike, spanOpen bool
	open := func() {
		var st string
		if fg != "" {
			st += "color:" + fg + ";"
		}
		if bg != "" {
			st += "background:" + bg + ";"
		}
		if bold {
			st += "font-weight:600;"
		}
		if ul {
			st += "text-decoration:underline;"
		}
		if strike {
			st += "text-decoration:line-through;"
		}
		if st != "" {
			b.WriteString(`<span style="` + st + `">`)
			spanOpen = true
		}
	}
	closeS := func() {
		if spanOpen {
			b.WriteString("</span>")
			spanOpen = false
		}
	}
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			closeS()
			applyCodes(s[i+2:j], &fg, &bg, &bold, &ul, &strike)
			open()
			i = j + 1
			continue
		}
		switch s[i] {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '\n':
			closeS()
			b.WriteByte('\n')
			open()
		default:
			b.WriteByte(s[i])
		}
		i++
	}
	closeS()
	return b.String()
}

func applyCodes(codes string, fg, bg *string, bold, ul, strike *bool) {
	toks := strings.Split(codes, ";")
	for k := 0; k < len(toks); k++ {
		switch toks[k] {
		case "0", "":
			*fg, *bg, *bold, *ul, *strike = "", "", false, false, false
		case "1":
			*bold = true
		case "22":
			*bold = false
		case "4":
			*ul = true
		case "24":
			*ul = false
		case "9":
			*strike = true
		case "29":
			*strike = false
		case "39":
			*fg = ""
		case "49":
			*bg = ""
		case "38":
			if k+4 < len(toks) && toks[k+1] == "2" {
				*fg = fmt.Sprintf("#%02x%02x%02x", atoiByte(toks[k+2]), atoiByte(toks[k+3]), atoiByte(toks[k+4]))
				k += 4
			}
		case "48":
			if k+4 < len(toks) && toks[k+1] == "2" {
				*bg = fmt.Sprintf("#%02x%02x%02x", atoiByte(toks[k+2]), atoiByte(toks[k+3]), atoiByte(toks[k+4]))
				k += 4
			}
		}
	}
}

func atoiByte(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
