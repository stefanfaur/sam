package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Accent          lipgloss.Color
	Muted           lipgloss.Color
	UserBorder      lipgloss.Color
	AssistantFg     lipgloss.Color
	ErrorFg         lipgloss.Color
	StateThinking   lipgloss.Color
	StateResponding lipgloss.Color
	StateTool       lipgloss.Color
	StateError      lipgloss.Color
	StateApproval   lipgloss.Color
	GlamourStyle    string

	StatusBar       lipgloss.Style
	InputBox        lipgloss.Style
	InputBoxFocus   lipgloss.Style
	InputPrompt     lipgloss.Style
	UserMsg         lipgloss.Style
	Info            lipgloss.Style
	ToolHeader      lipgloss.Style
	ToolInput       lipgloss.Style
	ToolResult      lipgloss.Style
	ToolError       lipgloss.Style
	DebugPanel      lipgloss.Style
	Thinking        lipgloss.Style
	ThinkingHeader  lipgloss.Style
	SkillCard       lipgloss.Style
	SkillCardHeader lipgloss.Style
	SkillCardMeta   lipgloss.Style
	SkillCardHint   lipgloss.Style
	Suggest         lipgloss.Style
	SuggestSelected lipgloss.Style

	glam *glamour.TermRenderer
}

func NewTheme(t ThemeSettings) *Theme {
	th := &Theme{
		Accent:          parseColor(t.Accent, "63"),
		Muted:           parseColor(t.Muted, "244"),
		UserBorder:      parseColor(t.UserBorder, "63"),
		AssistantFg:     parseColor(t.AssistantFg, ""),
		ErrorFg:         parseColor(t.ErrorFg, "160"),
		StateThinking:   parseColor(t.StateThinking, "39"),
		StateResponding: parseColor(t.StateResponding, "42"),
		StateTool:       parseColor(t.StateTool, "214"),
		StateError:      parseColor(t.StateError, "160"),
		StateApproval:   parseColor(t.StateApproval, "141"),
		GlamourStyle:    fallback(t.GlamourStyle, "dark"),
	}
	th.Apply(80)
	return th
}

func (t *Theme) Apply(width int) {
	if width <= 0 {
		width = 80
	}
	t.UserMsg = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, false, false, true).
		BorderForeground(t.UserBorder).
		Padding(0, 1).MarginTop(1).Bold(true)

	t.ToolHeader = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	t.ToolInput = lipgloss.NewStyle().Foreground(t.Muted).Italic(true)
	t.ToolResult = lipgloss.NewStyle().Foreground(t.Muted)
	t.ToolError = lipgloss.NewStyle().Foreground(t.ErrorFg)
	t.StatusBar = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252"))
	t.DebugPanel = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(t.Accent).
		Padding(1)
	t.Info = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Italic(true)
	t.Thinking = lipgloss.NewStyle().
		Foreground(t.Muted).Italic(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	t.ThinkingHeader = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Italic(true)
	t.SkillCard = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(0, 1).MarginTop(1)
	t.SkillCardHeader = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	t.SkillCardMeta = lipgloss.NewStyle().Foreground(t.Muted)
	t.SkillCardHint = lipgloss.NewStyle().Foreground(t.Muted).Italic(true)
	t.Suggest = lipgloss.NewStyle().Foreground(t.Muted)
	t.SuggestSelected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Bold(true)
	t.InputPrompt = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	t.InputBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(t.Muted).Padding(0, 1)
	t.InputBoxFocus = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent).Padding(0, 1)

	t.glam, _ = glamour.NewTermRenderer(
		glamour.WithStandardStyle(t.GlamourStyle),
		glamour.WithWordWrap(width),
	)
}

func (t *Theme) Glamour() *glamour.TermRenderer { return t.glam }

func parseColor(s, dflt string) lipgloss.Color {
	if s == "" {
		return lipgloss.Color(dflt)
	}
	return lipgloss.Color(s)
}

func fallback(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
