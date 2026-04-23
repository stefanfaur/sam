package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// segment is one styled chunk on the status bar. Higher priority = dropped
// first when overflowing. visible=false means render nothing.
type segment struct {
	text     string
	priority int
	visible  bool
}

// statusBarBg is the shared status bar background — kept stable across
// segments so inline foregrounds never break the bar's band.
const statusBarBg = lipgloss.Color("236")

// segStyle returns a style with the status bar background and the requested
// foreground, so segment text sits cleanly inside the bar.
func segStyle(fg lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(statusBarBg).Foreground(fg)
}

func segStyleBold(fg lipgloss.Color) lipgloss.Style {
	return segStyle(fg).Bold(true)
}

func stateLabel(state string) string {
	switch {
	case strings.HasPrefix(state, "tool:"):
		return "tool:" + strings.TrimPrefix(state, "tool:")
	case state == "thinking" || state == "thinking…":
		return "thinking"
	case state == "responding":
		return "responding"
	case state == "idle":
		return "idle"
	case state == "awaiting-approval":
		return "approval"
	case state == "error":
		return "error"
	case state == "cancelled":
		return "cancelled"
	}
	return state
}

func stateColor(t *Theme, state string) lipgloss.Color {
	switch {
	case strings.HasPrefix(state, "tool:"):
		return t.StateTool
	case strings.HasPrefix(state, "thinking"):
		return t.StateThinking
	case state == "responding":
		return t.StateResponding
	case state == "awaiting-approval":
		return t.StateApproval
	case state == "error":
		return t.StateError
	}
	return lipgloss.Color("252")
}

func segState(m *Model) segment {
	if !m.settings.Statusbar.Segments.State {
		return segment{}
	}
	label := stateLabel(m.status.state)
	if label == "" {
		return segment{}
	}
	var parts []string
	if m.spinner.on && m.settings.Statusbar.Spinner {
		parts = append(parts, m.spinner.glyph())
	}
	parts = append(parts, label)
	body := strings.Join(parts, " ")
	color := lipgloss.Color("252")
	if m.settings.Statusbar.Colors {
		color = stateColor(m.theme, m.status.state)
	}
	return segment{text: segStyleBold(color).Render(body), priority: 1, visible: true}
}

func segElapsed(m *Model) segment {
	if !m.settings.Statusbar.Elapsed || m.pending == nil || m.turnStart.IsZero() {
		return segment{}
	}
	d := time.Since(m.turnStart).Round(100 * time.Millisecond)
	return segment{
		text:     segStyle(lipgloss.Color("244")).Render(d.String()),
		priority: 5,
		visible:  true,
	}
}

func segIter(m *Model) segment {
	if !m.settings.Statusbar.Segments.Iterations {
		return segment{}
	}
	if m.status.maxIter == 0 {
		return segment{}
	}
	text := fmt.Sprintf("↻ %d/%d", m.status.iter, m.status.maxIter)
	return segment{
		text:     segStyle(lipgloss.Color("110")).Render(text),
		priority: 4,
		visible:  true,
	}
}

func segModel(m *Model) segment {
	if !m.settings.Statusbar.Segments.Model || m.status.model == "" {
		return segment{}
	}
	return segment{
		text:     segStyleBold(lipgloss.Color("117")).Render(m.status.model),
		priority: 2,
		visible:  true,
	}
}

func segProvider(m *Model) segment {
	if !m.settings.Statusbar.Segments.Provider || m.status.provider == "" {
		return segment{}
	}
	return segment{
		text:     segStyleBold(m.theme.Accent).Render(m.status.provider),
		priority: 3,
		visible:  true,
	}
}

func segCwd(m *Model) segment {
	if !m.settings.Statusbar.Segments.Cwd || m.agent == nil {
		return segment{}
	}
	dir := m.agent.LaunchDir()
	if dir == "" {
		return segment{}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	if len(dir) > 30 {
		base := filepath.Base(dir)
		dir = "…/" + base
	}
	return segment{
		text:     segStyle(lipgloss.Color("245")).Render(dir),
		priority: 6,
		visible:  true,
	}
}

func segGit(m *Model) segment {
	if !m.settings.Statusbar.Segments.Git || m.git.branch == "" {
		return segment{}
	}
	branchColor := lipgloss.Color("151") // soft green
	if m.git.dirty {
		branchColor = lipgloss.Color("215") // soft amber when dirty
	}
	text := " " + m.git.branch
	if m.git.dirty {
		text += " ●"
	}
	return segment{
		text:     segStyle(branchColor).Render(text),
		priority: 7,
		visible:  true,
	}
}

func segCtx(m *Model, ctxWindow int) segment {
	if !m.settings.Statusbar.Segments.Context || ctxWindow <= 0 {
		return segment{}
	}
	if m.status.lastIterIn == 0 {
		return segment{
			text:     segStyle(lipgloss.Color("244")).Render("ctx —"),
			priority: 8,
			visible:  true,
		}
	}
	pct := int(float64(m.status.lastIterIn) / float64(ctxWindow) * 100)
	color := lipgloss.Color("151")
	switch {
	case pct >= 80:
		color = lipgloss.Color("203")
	case pct >= 50:
		color = lipgloss.Color("215")
	}
	return segment{
		text:     segStyle(color).Render(fmt.Sprintf("ctx %d%%", pct)),
		priority: 8,
		visible:  true,
	}
}

func segTokens(m *Model) segment {
	if !m.settings.Statusbar.Segments.Tokens {
		return segment{}
	}
	if m.status.turnIn == 0 && m.status.turnOut == 0 {
		return segment{}
	}
	inStyle := segStyle(lipgloss.Color("110"))
	outStyle := segStyle(lipgloss.Color("117"))
	dimStyle := segStyle(lipgloss.Color("244"))
	cacheStyle := segStyle(lipgloss.Color("108"))

	effectiveIn := m.status.turnIn - m.status.turnCacheRead
	if effectiveIn < 0 {
		effectiveIn = 0
	}
	text := inStyle.Render(humanK(effectiveIn)+"↓") +
		dimStyle.Render(" ") +
		outStyle.Render(humanK(m.status.turnOut)+"↑")
	if m.status.turnCacheRead > 0 {
		text += dimStyle.Render(" ") +
			cacheStyle.Render(humanK(m.status.turnCacheRead)+"⚡")
	}
	return segment{text: text, priority: 9, visible: true}
}

func humanK(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
