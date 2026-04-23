package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/stefanfaur/sam/internal/agent"
)

func TestIntegration_CardsFullTurnLifecycle(t *testing.T) {
	m := &Model{
		pending: synthPending(),
		theme:   NewTheme(DefaultSettings().Theme),
		scanner: &blockScanner{},
		input:   textarea.New(),
		width:   80,
		settings: Settings{
			Thinking: ThinkingSettings{StreamMode: "full"},
		},
	}

	// 1. Thinking deltas — card materializes, view includes body.
	m.applyAgentEvent(agent.ThinkingDelta{Text: "reading plan"})
	m.applyAgentEvent(agent.ThinkingDelta{Text: ", now drafting"})
	if m.pending.thinking == nil || !strings.Contains(m.pending.thinking.Text, "reading plan") {
		t.Fatalf("thinking not populated: %+v", m.pending.thinking)
	}
	view1 := m.View()
	if !strings.Contains(view1, "reading plan") {
		t.Errorf("view missing thinking body: %q", view1)
	}

	// 2. TextDelta settles thinking.
	m.applyAgentEvent(agent.TextDelta{Text: "hello"})
	if m.pending.thinking.EndedAt.IsZero() {
		t.Error("TextDelta should settle thinking")
	}

	// 3. ToolCall appends card.
	m.applyAgentEvent(agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)})
	if len(m.pending.tools) != 1 || m.pending.tools[0].ID != "t1" {
		t.Fatalf("tool card missing: %+v", m.pending.tools)
	}
	view2 := m.View()
	if !strings.Contains(view2, "Bash") {
		t.Errorf("view missing tool card: %q", view2)
	}

	// 4. ToolResult fills output + Lines + EndedAt.
	m.applyAgentEvent(agent.ToolResult{ID: "t1", Name: "Bash", Output: "a\nb\n"})
	tc := m.pending.tools[0]
	if tc.Output == "" || tc.Lines != 2 || tc.EndedAt.IsZero() {
		t.Errorf("tool result not applied: %+v", tc)
	}

	// 5. Flush produces thinking + tool settled; rings populated at Index 1.
	settled := m.flushTurnSettled()
	if len(settled) != 2 {
		t.Fatalf("expected 2 settled strings, got %d", len(settled))
	}
	if !strings.Contains(settled[0], "✧") {
		t.Errorf("settled[0] not thinking: %q", settled[0])
	}
	if !strings.Contains(settled[1], "Bash") {
		t.Errorf("settled[1] not tool: %q", settled[1])
	}
	if len(m.recentTools) != 1 || m.recentTools[0].Index != 1 {
		t.Errorf("recentTools wrong: %+v", m.recentTools)
	}
	if len(m.recentThinking) != 1 || m.recentThinking[0].Index != 1 {
		t.Errorf("recentThinking wrong: %+v", m.recentThinking)
	}

	_ = time.Now()
}
