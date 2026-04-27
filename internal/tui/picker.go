package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/stefanfaur/sam/internal/agent"
)

// rewindPickerEntry is the picker-side projection of an agent.TurnRecord. We
// keep it separate from the persisted record so we can synthesize lightweight
// labels for the live in-memory turn count when no sidecar exists yet.
type rewindPickerEntry struct {
	Index       int
	UserMsg     string
	Tools       []agent.ToolCallRecord
	SnapshotSHA string
	StoppedBy   string
	Timestamp   time.Time
	// HasBash indicates whether any tool call in this turn was a Bash
	// invocation — surfaced as a warning glyph in the preview because Bash
	// side-effects (network, processes) cannot be rolled back via checkpoint.
	HasBash bool
}

// rewindPicker is the model for the Esc-Esc time-travel picker. It is a plain
// struct (not a tea.Model) so it can be unit-tested without the full Bubbletea
// runtime. The owning Model decides when to draw it via View() and routes
// keystrokes via the helpers below.
type rewindPicker struct {
	entries  []rewindPickerEntry
	filtered []rewindPickerEntry
	query    string
	selected int
}

// newRewindPicker initializes a picker over the supplied turn records,
// preserving plan order (turn index ascending). The most recent turn is
// pre-selected so Esc-Esc → Enter rewinds one turn back by default.
func newRewindPicker(records []agent.TurnRecord) *rewindPicker {
	entries := make([]rewindPickerEntry, 0, len(records))
	for _, r := range records {
		hasBash := false
		for _, t := range r.Tools {
			if t.Name == "Bash" {
				hasBash = true
				break
			}
		}
		entries = append(entries, rewindPickerEntry{
			Index:       r.Index,
			UserMsg:     r.UserMsg,
			Tools:       r.Tools,
			SnapshotSHA: r.SnapshotSHA,
			StoppedBy:   r.StoppedBy,
			Timestamp:   r.Timestamp,
			HasBash:     hasBash,
		})
	}
	p := &rewindPicker{entries: entries}
	p.applyFilter()
	if n := len(p.filtered); n > 0 {
		p.selected = n - 1
	}
	return p
}

// SetQuery updates the filter query and re-applies the fuzzy matcher.
func (p *rewindPicker) SetQuery(q string) {
	p.query = q
	p.applyFilter()
	if p.selected >= len(p.filtered) {
		p.selected = len(p.filtered) - 1
	}
	if p.selected < 0 {
		p.selected = 0
	}
}

// Move shifts the selection by delta, clamped to the filtered range.
func (p *rewindPicker) Move(delta int) {
	n := len(p.filtered)
	if n == 0 {
		p.selected = 0
		return
	}
	p.selected += delta
	if p.selected < 0 {
		p.selected = 0
	}
	if p.selected >= n {
		p.selected = n - 1
	}
}

// Current returns the highlighted entry. The bool is false if no entries match.
func (p *rewindPicker) Current() (rewindPickerEntry, bool) {
	if p.selected < 0 || p.selected >= len(p.filtered) {
		return rewindPickerEntry{}, false
	}
	return p.filtered[p.selected], true
}

// Filtered returns the currently-visible entries in display order.
func (p *rewindPicker) Filtered() []rewindPickerEntry { return p.filtered }

func (p *rewindPicker) applyFilter() {
	if p.query == "" {
		p.filtered = append([]rewindPickerEntry(nil), p.entries...)
		return
	}
	// Fuzzy-match on the user message text only — that's the most useful
	// pivot for "find the turn where I asked for X".
	corpus := make([]string, len(p.entries))
	for i, e := range p.entries {
		corpus[i] = e.UserMsg
	}
	matches := fuzzy.Find(p.query, corpus)
	out := make([]rewindPickerEntry, 0, len(matches))
	for _, m := range matches {
		out = append(out, p.entries[m.Index])
	}
	p.filtered = out
}

// Render draws the full-screen split: turn list (left) + preview (right).
// Width/height are the terminal dimensions; the caller handles centering/clearing.
func (p *rewindPicker) Render(t *Theme, width, height int) string {
	if p == nil {
		return ""
	}
	if width < 40 || height < 10 {
		return t.Suggest.Render("rewind: terminal too small")
	}

	headerStyle := t.ThinkingHeader
	hintStyle := t.Suggest
	selectedStyle := t.SuggestSelected
	rowStyle := t.Suggest

	header := headerStyle.Render(fmt.Sprintf("Rewind  filter:%q  (%d/%d)",
		p.query, len(p.filtered), len(p.entries)))
	hint := hintStyle.Render("↑↓ select · / filter · Enter open · Esc cancel")

	listW := width / 3
	if listW < 24 {
		listW = 24
	}
	previewW := width - listW - 3
	if previewW < 20 {
		previewW = 20
	}
	bodyH := height - 4 // header + hint + 2 padding

	// Build list column.
	var listLines []string
	for i, e := range p.filtered {
		row := fmt.Sprintf("%2d  %s", e.Index, oneLine(e.UserMsg, listW-6))
		if e.HasBash {
			row += " ⚠"
		}
		if i == p.selected {
			listLines = append(listLines, selectedStyle.Render("› "+row))
		} else {
			listLines = append(listLines, rowStyle.Render("  "+row))
		}
	}
	for len(listLines) < bodyH {
		listLines = append(listLines, "")
	}
	if len(listLines) > bodyH {
		listLines = listLines[:bodyH]
	}
	listCol := strings.Join(listLines, "\n")

	// Build preview column.
	preview := p.renderPreview(t, previewW, bodyH)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listW).Render(listCol),
		hintStyle.Render(" │ "),
		lipgloss.NewStyle().Width(previewW).Render(preview),
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", hint)
}

// renderPreview formats the right pane: user message + tool list + stop
// reason + bash warning if any.
func (p *rewindPicker) renderPreview(t *Theme, width, height int) string {
	cur, ok := p.Current()
	if !ok {
		return t.Suggest.Render("(no turn selected)")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Turn %d", cur.Index)
	if !cur.Timestamp.IsZero() {
		fmt.Fprintf(&b, "  (%s)", cur.Timestamp.Local().Format("15:04:05"))
	}
	b.WriteString("\n\n")
	b.WriteString("user: ")
	b.WriteString(truncateBlock(cur.UserMsg, width-6, 6))
	b.WriteString("\n\n")
	if len(cur.Tools) > 0 {
		b.WriteString("tools:\n")
		for _, t := range cur.Tools {
			marker := " "
			if t.IsError {
				marker = "✗"
			}
			fmt.Fprintf(&b, "  %s %s  %s\n", marker, t.Name, oneLine(string(t.Input), width-12))
		}
		b.WriteString("\n")
	}
	if cur.HasBash {
		b.WriteString(t.Suggest.Render("⚠ contains Bash — side-effects cannot be rolled back\n"))
	}
	if cur.StoppedBy != "" {
		fmt.Fprintf(&b, "\nstop: %s\n", cur.StoppedBy)
	}
	return b.String()
}

// oneLine collapses whitespace and truncates s to width with an ellipsis.
func oneLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return s[:width-1] + "…"
}

// truncateBlock wraps s to width, capping at maxLines lines (extra lines
// collapse to a trailing "…").
func truncateBlock(s string, width, maxLines int) string {
	if width <= 0 || maxLines <= 0 {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		for len(line) > width {
			lines = append(lines, line[:width])
			line = line[width:]
			if len(lines) >= maxLines {
				break
			}
		}
		if len(lines) >= maxLines {
			break
		}
		lines = append(lines, line)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = strings.TrimRight(lines[maxLines-1], " ") + "…"
	}
	return strings.Join(lines, "\n")
}
