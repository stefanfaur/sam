package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders one or two styled rows per Settings. Returns empty when
// disabled. Caller is responsible for joining rows into the view.
func (m *Model) StatusBar() []string {
	if !m.settings.Statusbar.Enabled {
		return nil
	}
	ctxMax := 0
	if m.ctxWinFn != nil {
		ctxMax = m.ctxWinFn(m.status.model)
	}

	top := []segment{
		segState(m),
		segElapsed(m),
		segIter(m),
	}
	bottom := []segment{
		segProvider(m),
		segModel(m),
		segCwd(m),
		segGit(m),
		segCtx(m, ctxMax),
		segTokens(m),
	}

	width := m.width
	if width <= 0 {
		width = 80
	}

	rows := []string{}
	switch m.settings.Statusbar.Layout {
	case "one-line":
		all := append([]segment{}, top...)
		all = append(all, bottom...)
		if row := composeRow(all, width); row != "" {
			rows = append(rows, padRow(row, width))
		}
	default: // two-line
		if row := composeRow(top, width); row != "" {
			rows = append(rows, padRow(row, width))
		}
		if row := composeRow(bottom, width); row != "" {
			rows = append(rows, padRow(row, width))
		}
	}
	return rows
}

func composeRow(segs []segment, maxW int) string {
	active := make([]segment, 0, len(segs))
	for _, s := range segs {
		if s.visible {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		return ""
	}
	for {
		joined := joinSegs(active)
		if lipgloss.Width(joined) <= maxW || len(active) == 1 {
			return joined
		}
		active = dropHighestPriority(active)
	}
}

func joinSegs(segs []segment) string {
	pad := segStyle(lipgloss.Color("244")).Render(" ")
	sep := segStyle(lipgloss.Color("240")).Render(" · ")
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, s.text)
	}
	return pad + strings.Join(parts, sep) + pad
}

// padRow extends row with bg-colored spaces to fill width. Keeping the
// padding styled with the shared status bar bg avoids the "hole" effect
// where inner ANSI resets leave the terminal default showing through.
func padRow(row string, width int) string {
	gap := width - lipgloss.Width(row)
	if gap <= 0 {
		return row
	}
	return row + segStyle(lipgloss.Color("244")).Render(strings.Repeat(" ", gap))
}

// dropHighestPriority removes the segment whose priority number is largest.
// Priority 1 is kept longest; higher numbers drop first.
func dropHighestPriority(segs []segment) []segment {
	if len(segs) == 0 {
		return segs
	}
	idx := 0
	for i, s := range segs {
		if s.priority > segs[idx].priority {
			idx = i
		}
	}
	out := make([]segment, 0, len(segs)-1)
	out = append(out, segs[:idx]...)
	out = append(out, segs[idx+1:]...)
	return out
}
