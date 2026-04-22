package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

func renderToolCall(name string, input json.RawMessage) string {
	preview := toolInputPreview(name, input)
	return toolHeaderStyle.Render("● "+name) + "  " + toolInputStyle.Render(preview)
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

func renderToolResult(name, output string, isError bool) string {
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
		return toolErrorStyle.Render("✗ "+name+":\n") + toolErrorStyle.Render(body)
	}
	return toolResultStyle.Render(body)
}

func renderError(err error) string {
	if err == nil {
		return ""
	}
	return toolErrorStyle.Render("✗ error: " + err.Error())
}
