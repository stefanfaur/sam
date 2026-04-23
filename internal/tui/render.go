package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func renderToolCall(t *Theme, name string, input json.RawMessage) string {
	preview := toolInputPreview(name, input)
	return t.ToolHeader.Render("● "+name) + "  " + t.ToolInput.Render(preview)
}

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

func renderToolResult(t *Theme, name, output string, isError bool) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	limit := 10
	more := 0
	if len(lines) > limit {
		more = len(lines) - limit
		lines = lines[:limit]
	}
	body := strings.Join(lines, "\n")
	if more > 0 {
		body += fmt.Sprintf("\n… %d more lines", more)
	}
	if isError {
		return t.ToolError.Render("✗ "+name+":\n") + t.ToolError.Render(body)
	}
	return t.ToolResult.Render(body)
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

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

func renderError(t *Theme, err error) string {
	if err == nil {
		return ""
	}
	return t.ToolError.Render("✗ error: " + err.Error())
}
