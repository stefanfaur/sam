package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func (s statusbarModel) View() string {
	text := fmt.Sprintf(" %s · %s · %s · %d/%d · %d in / %d out ",
		s.provider, s.model, s.state, s.iter, s.maxIter, s.inputTokens, s.outputTokens)
	style := statusBarStyle
	if s.width > 0 {
		style = style.Width(s.width)
	} else {
		style = style.Width(lipgloss.Width(text))
	}
	return style.Render(text)
}
