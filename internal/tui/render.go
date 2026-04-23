package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func toolInputPreview(name string, input json.RawMessage) string {
	var data map[string]any
	_ = json.Unmarshal(input, &data)
	switch name {
	case "Bash":
		if cmd, ok := data["command"].(string); ok {
			return truncate(strings.ReplaceAll(cmd, "\n", " ⏎ "), 100)
		}
	case "Write":
		if fp, ok := data["file_path"].(string); ok {
			c, _ := data["content"].(string)
			return fmt.Sprintf("%s (%d bytes)", fp, len(c))
		}
	case "Edit":
		if fp, ok := data["file_path"].(string); ok {
			o, _ := data["old_string"].(string)
			n, _ := data["new_string"].(string)
			return fmt.Sprintf("%s: %q → %q", fp, truncate(o, 40), truncate(n, 40))
		}
	case "Read":
		if fp, ok := data["file_path"].(string); ok {
			return fp
		}
	}
	raw := string(input)
	return truncate(raw, 100)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// renderSkillCard paints the skill invocation card. When `live` is true the
// header includes an animated spinner glyph; when false a settled "●" marker
// is used for the final static render flushed to scrollback.
func renderSkillCard(t *Theme, sc *skillCardState, elapsed time.Duration, spinnerFrame int, live bool) string {
	if sc == nil {
		return ""
	}
	glyph := "●"
	if live {
		glyph = spinnerFrames[spinnerFrame%len(spinnerFrames)]
	}
	header := t.SkillCardHeader.Render(
		fmt.Sprintf("%s skill %s", glyph, sc.Header),
	)
	meta := t.SkillCardMeta.Render(
		fmt.Sprintf("source: %s · prompt: %d chars · ~%d tok · %s",
			sc.Source,
			len(sc.Body),
			estimateTUITokens(sc.Body),
			formatElapsed(elapsed),
		),
	)
	hint := t.SkillCardHint.Render(
		fmt.Sprintf("[/show-skill %d] to expand body", sc.Index),
	)
	body := header + "\n" + meta + "\n" + hint
	return t.SkillCard.Render(body)
}

func estimateTUITokens(s string) int { return (len(s) + 3) / 4 }

// toolSummary returns the meta slug used in a tool card header.
// Running cards return "running…"; cancelled cards return "cancelled · <elapsed>";
// otherwise per-tool summary per the design spec.
func toolSummary(tc *toolCardState, now time.Time) string {
	if tc.EndedAt.IsZero() && !tc.Cancelled {
		return "running…"
	}
	elapsed := formatElapsed(tc.EndedAt.Sub(tc.StartedAt))
	if tc.Cancelled {
		return fmt.Sprintf("cancelled · %s", elapsed)
	}
	switch tc.Name {
	case "Edit":
		base := toolInputBasename(tc.Input)
		if base == "" {
			return fmt.Sprintf("%d lines edited · %s", tc.EditLines, elapsed)
		}
		return fmt.Sprintf("%d lines edited · %s", tc.EditLines, base)
	case "Write":
		base := toolInputBasename(tc.Input)
		if base == "" {
			return fmt.Sprintf("%d lines written · %s", tc.Lines, elapsed)
		}
		return fmt.Sprintf("%d lines written · %s", tc.Lines, base)
	case "Read":
		base := toolInputBasename(tc.Input)
		if base == "" {
			return fmt.Sprintf("%d lines · %s", tc.Lines, elapsed)
		}
		return fmt.Sprintf("%d lines · %s · %s", tc.Lines, elapsed, base)
	case "Task":
		tok := (tc.Bytes + 3) / 4
		return fmt.Sprintf("~%d tok · %s", tok, elapsed)
	default:
		return fmt.Sprintf("%d lines · %s", tc.Lines, elapsed)
	}
}

func toolInputBasename(input json.RawMessage) string {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return ""
	}
	fp, _ := data["file_path"].(string)
	if fp == "" {
		return ""
	}
	return filepath.Base(fp)
}

// toolInputLine returns a width-trimmed preview of the tool input suitable for
// the card meta row.
func toolInputLine(name string, input json.RawMessage, width int) string {
	preview := toolInputPreview(name, input)
	if width > 0 && lipgloss.Width(preview) > width {
		if width > 1 {
			return truncate(preview, width-1)
		}
		return truncate(preview, width)
	}
	return preview
}

// renderToolCard renders a tool card. Running state uses a bordered card for
// live visibility; settled/error/cancelled states render as a compact
// borderless one-liner (with an optional second indented peek for errors or
// Bash) so multiple cards stack tightly in scrollback.
func renderToolCard(t *Theme, tc *toolCardState, now time.Time, spinnerFrame, innerWidth int) string {
	if tc == nil {
		return ""
	}
	if innerWidth <= 0 {
		innerWidth = 80
	}
	if tc.EndedAt.IsZero() && !tc.Cancelled {
		return renderToolCardRunning(t, tc, spinnerFrame, innerWidth)
	}
	return renderToolCardSettled(t, tc, now, innerWidth)
}

func renderToolCardRunning(t *Theme, tc *toolCardState, spinnerFrame, innerWidth int) string {
	glyph := spinnerFrames[spinnerFrame%len(spinnerFrames)]
	header := t.ToolCardHeader.Render(fmt.Sprintf("%s %s · running…", glyph, tc.Name))
	meta := t.ToolCardMeta.Render(toolInputLine(tc.Name, tc.Input, innerWidth-2))
	return t.ToolCard.Render(header + "\n" + meta)
}

func renderToolCardSettled(t *Theme, tc *toolCardState, now time.Time, innerWidth int) string {
	var glyph string
	switch {
	case tc.Cancelled:
		glyph = "◌"
	case tc.IsError:
		glyph = "✗"
	default:
		glyph = "●"
	}
	// Head style: dim glyph + accent name + muted summary.
	glyphSty := t.ToolCardMeta
	nameSty := t.ToolCardHeader
	if tc.IsError {
		glyphSty = t.ToolCardPeekErr
		nameSty = lipgloss.NewStyle().Foreground(t.ErrorFg).Bold(true)
	}

	summary := toolSummary(tc, now)
	detail := settledToolDetail(tc)
	hintText := fmt.Sprintf("[%d]", tc.Index)

	head := glyphSty.Render(glyph) + " " +
		nameSty.Render(tc.Name) + " " +
		t.ToolCardMeta.Render("· "+summary)
	if detail != "" {
		head += " " + t.ToolCardMeta.Render("· "+detail)
	}
	hint := t.ToolCardMeta.Render(hintText)

	// Trim the head if it would push the hint off the terminal; always show hint.
	room := innerWidth - lipgloss.Width(hint) - 2
	if room > 0 && lipgloss.Width(head) > room {
		// Can't easily truncate a styled string without breaking ANSI; the
		// cheapest safe option is to let lipgloss soft-wrap the rendered head,
		// then append the hint on its own line.
		return head + "\n" + hint + maybeErrorPeek(t, tc, innerWidth)
	}
	line := head + "  " + hint
	return line + maybeErrorPeek(t, tc, innerWidth)
}

// settledToolDetail returns an optional per-tool detail suffix (e.g. the Bash
// command) for the settled one-liner. Empty when the summary already conveys
// enough context.
func settledToolDetail(tc *toolCardState) string {
	if tc.Cancelled {
		return ""
	}
	switch tc.Name {
	case "Bash":
		var data map[string]any
		_ = json.Unmarshal(tc.Input, &data)
		if cmd, ok := data["command"].(string); ok {
			return truncate(strings.ReplaceAll(cmd, "\n", " ⏎ "), 60)
		}
	}
	return ""
}

// maybeErrorPeek returns an indented error-colored second line with the first
// line of tc.Output when tc is an error. Otherwise empty string.
func maybeErrorPeek(t *Theme, tc *toolCardState, innerWidth int) string {
	if !tc.IsError || tc.Output == "" {
		return ""
	}
	first := strings.SplitN(tc.Output, "\n", 2)[0]
	if innerWidth > 4 {
		first = truncate(first, innerWidth-4)
	}
	return "\n  " + t.ToolCardPeekErr.Render(first)
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// thinkingGlyph marks a settled thinking block. Uses a terminal-safe glyph
// (not an emoji) so the card renders consistently across fonts.
const thinkingGlyph = "✧"

// renderThinkingCard renders a thinking card. When live (EndedAt zero) the
// body reflects the selected mode ("full" or "header"); once settled the card
// collapses to a single-line ✧ marker and mode is ignored.
func renderThinkingCard(t *Theme, tc *thinkingCardState, now time.Time,
	spinnerFrame int, mode string, innerWidth int) string {
	if tc == nil {
		return ""
	}
	if innerWidth <= 0 {
		innerWidth = 80
	}

	if !tc.EndedAt.IsZero() {
		// Settled thinking: compact borderless one-liner.
		elapsed := formatElapsed(tc.EndedAt.Sub(tc.StartedAt))
		head := t.ToolCardMeta.Render(thinkingGlyph) + " " +
			t.ToolCardMeta.Italic(true).Render(
				fmt.Sprintf("thought for %s · %d chars", elapsed, len(tc.Text)),
			)
		hint := t.ToolCardMeta.Render(fmt.Sprintf("[%d]", tc.Index))
		return head + "  " + hint
	}

	spinner := spinnerFrames[spinnerFrame%len(spinnerFrames)]
	if mode == "header" {
		header := t.ToolCardHeader.Render(
			fmt.Sprintf("%s thinking… %d chars", spinner, len(tc.Text)),
		)
		return t.ThinkingCard.Render(header)
	}
	// Full mode (default).
	header := t.ToolCardHeader.Render(fmt.Sprintf("%s thinking…", spinner))
	body := t.ThinkingCardBody.Render(strings.TrimRight(tc.Text, "\n"))
	return t.ThinkingCard.Render(header + "\n" + body)
}

func renderError(t *Theme, err error) string {
	if err == nil {
		return ""
	}
	return t.ToolError.Render("✗ error: " + err.Error())
}
