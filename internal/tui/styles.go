package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	userMsgStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1).
			MarginBottom(1).
			Bold(true)

	assistantStyle = lipgloss.NewStyle()

	toolHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	toolInputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("160"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252"))

	debugPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Italic(true)

	thinkingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)

	thinkingHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Bold(true).
			Italic(true)

	suggestStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	suggestSelectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("238")).
			Bold(true)

	inputPromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("63")).
			Bold(true)

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	inputBoxFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)
)

func glamourForWidth(w int) (*glamour.TermRenderer, error) {
	if w <= 0 {
		w = 80
	}
	return glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(w))
}
