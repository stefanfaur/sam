package tui

import (
	"strings"
	"testing"
)

func TestParseCommand_ShowTool(t *testing.T) {
	cmd, arg, _ := parseCommand("/show-tool 7", nil)
	if cmd != CmdShowTool || arg != "7" {
		t.Errorf("got (%q,%q)", cmd, arg)
	}
	cmd, arg, _ = parseCommand("/SHOW-TOOL", nil)
	if cmd != CmdShowTool || arg != "" {
		t.Errorf("case-insensitive no-arg: (%q,%q)", cmd, arg)
	}
}

func TestParseCommand_ShowThinking(t *testing.T) {
	cmd, arg, _ := parseCommand("/show-thinking 3", nil)
	if cmd != CmdShowThinking || arg != "3" {
		t.Errorf("got (%q,%q)", cmd, arg)
	}
}

func TestCommandSuggestions_IncludesShowToolAndThinking(t *testing.T) {
	sugg := suggestionsFor(nil)
	var seenT, seenTh bool
	for _, s := range sugg {
		if strings.HasPrefix(s.Name, "/show-tool") {
			seenT = true
		}
		if strings.HasPrefix(s.Name, "/show-thinking") {
			seenTh = true
		}
	}
	if !seenT || !seenTh {
		t.Errorf("missing suggestions: tool=%v thinking=%v", seenT, seenTh)
	}
}

func TestShowTool_MonotonicResolve(t *testing.T) {
	m := &Model{theme: NewTheme(DefaultSettings().Theme)}
	m.recordTool(toolInvocation{Index: 5, Header: "Bash · 1 line · 1s", Input: "go test", Output: "ok\n"})
	out := m.resolveToolDump(5)
	if !strings.Contains(out, "Bash") || !strings.Contains(out, "ok") {
		t.Errorf("dump missing content: %q", out)
	}
	if missing := m.resolveToolDump(999); !strings.Contains(missing, "unknown") {
		t.Errorf("unknown index should produce error line: %q", missing)
	}
}
