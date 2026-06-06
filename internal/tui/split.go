package tui

// Wide-mode list/detail divider: the list takes splitPct of the terminal width,
// resizable with the ‹ › keys or by dragging the divider with the mouse.
const (
	splitDefault = 58
	splitMin     = 25
	splitMax     = 80
)

// clampSplit defaults a zero percentage and bounds it to a sane range.
func clampSplit(pct int) int {
	switch {
	case pct == 0:
		return splitDefault // unset → default until the user resizes
	case pct < splitMin:
		return splitMin
	case pct > splitMax:
		return splitMax
	default:
		return pct
	}
}

// splitWidth is the wide-mode list width in columns; the divider sits here.
func (m *Model) splitWidth() int {
	return m.width * clampSplit(m.splitPct) / 100
}

// nudgeSplit shifts the divider by delta percentage points (keyboard ‹ ›).
func (m *Model) nudgeSplit(delta int) {
	m.splitPct = clampSplit(clampSplit(m.splitPct) + delta)
}

// setSplitFromX pins the divider to a dragged column (mouse).
func (m *Model) setSplitFromX(x int) {
	if m.width > 0 {
		m.splitPct = clampSplit(x * 100 / m.width)
	}
}
