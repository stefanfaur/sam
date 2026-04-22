package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (d debugModel) View(width, height int) string {
	if d.ring == nil {
		return debugPanelStyle.Render("debug panel: no ring configured")
	}
	entries := d.ring.Entries()
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("debug log (Ctrl+L to close)"))
	sb.WriteString("\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "%s %5s %s", e.Time.Format("15:04:05.000"), e.Level, e.Message)
		if len(e.Attrs) > 0 {
			for k, v := range e.Attrs {
				fmt.Fprintf(&sb, " %s=%v", k, v)
			}
		}
		sb.WriteString("\n")
	}
	w := width - 4
	h := height - 4
	if w < 20 {
		w = 20
	}
	if h < 5 {
		h = 5
	}
	return debugPanelStyle.Width(w).Height(h).Render(sb.String())
}
