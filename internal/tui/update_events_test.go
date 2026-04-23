package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/agent"
)

func synthPending() *pendingTurn {
	return &pendingTurn{events: make(chan Event)}
}

func TestHandleToolCall_AppendsCardAndSettlesThinking(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.pending.thinking = &thinkingCardState{StartedAt: time.Now().Add(-time.Second)}
	ev := agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)}
	m.applyAgentEvent(ev)
	if len(m.pending.tools) != 1 || m.pending.tools[0].ID != "t1" {
		t.Fatalf("tool not appended: %+v", m.pending.tools)
	}
	if m.pending.thinking.EndedAt.IsZero() {
		t.Error("thinking should settle on ToolCall arrival")
	}
	if m.pending.tools[0].Index != 1 {
		t.Errorf("tool Index = %d, want 1", m.pending.tools[0].Index)
	}
}

func TestHandleToolResult_MatchesByID(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.pending.tools = []*toolCardState{
		{ID: "t1", Name: "Bash", StartedAt: time.Now()},
	}
	ev := agent.ToolResult{ID: "t1", Name: "Bash", Output: "line1\nline2\n", IsError: false}
	m.applyAgentEvent(ev)
	if m.pending.tools[0].Output == "" {
		t.Error("output not filled")
	}
	if m.pending.tools[0].Lines != 2 {
		t.Errorf("Lines = %d, want 2", m.pending.tools[0].Lines)
	}
	if m.pending.tools[0].EndedAt.IsZero() {
		t.Error("EndedAt not set")
	}
}

func TestHandleToolResult_UnknownIDDefensive(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	ev := agent.ToolResult{ID: "ghost", Name: "Bash", Output: "x\n"}
	m.applyAgentEvent(ev)
	if len(m.pending.tools) != 1 {
		t.Fatalf("defensive card not created: %+v", m.pending.tools)
	}
	if !m.pending.tools[0].EndedAt.After(time.Time{}) {
		t.Error("defensive card should be settled")
	}
}

func TestHandleThinkingDelta_LazyAlloc(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.applyAgentEvent(agent.ThinkingDelta{Text: "hello"})
	if m.pending.thinking == nil || m.pending.thinking.Text != "hello" {
		t.Fatalf("thinking not allocated: %+v", m.pending.thinking)
	}
	if m.pending.thinking.Index != 1 {
		t.Errorf("Index = %d, want 1", m.pending.thinking.Index)
	}
}

func TestHandleTextDelta_SettlesThinking(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme), scanner: &blockScanner{}}
	m.pending.thinking = &thinkingCardState{StartedAt: time.Now().Add(-time.Second)}
	m.applyAgentEvent(agent.TextDelta{Text: "ok"})
	if m.pending.thinking.EndedAt.IsZero() {
		t.Error("thinking should settle on first text delta")
	}
}

func TestFlushOrder_ThinkingThenToolsThenText(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.applyAgentEvent(agent.ThinkingDelta{Text: "planning"})
	m.applyAgentEvent(agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)})
	m.applyAgentEvent(agent.ToolResult{ID: "t1", Name: "Bash", Output: "ok\n"})
	m.applyAgentEvent(agent.ToolCall{ID: "t2", Name: "Read", Input: json.RawMessage(`{}`)})
	m.applyAgentEvent(agent.ToolResult{ID: "t2", Name: "Read", Output: "a\nb\n"})
	out := m.flushTurnSettled()
	if len(out) < 3 {
		t.Fatalf("expected >=3 settled outputs, got %d: %v", len(out), out)
	}
	if !strings.Contains(out[0], "✧") {
		t.Errorf("out[0] not thinking: %q", out[0])
	}
	if !strings.Contains(out[1], "Bash") || !strings.Contains(out[2], "Read") {
		t.Errorf("tool order wrong: %q %q", out[1], out[2])
	}
	if len(m.recentTools) != 2 || len(m.recentThinking) != 1 {
		t.Errorf("rings not populated: tools=%d thinking=%d",
			len(m.recentTools), len(m.recentThinking))
	}
}

func TestFlush_CancelsRunningTool(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.applyAgentEvent(agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)})
	out := m.flushTurnSettled()
	if len(out) != 1 || !strings.Contains(out[0], "cancelled") {
		t.Errorf("cancelled card not flushed: %v", out)
	}
	if !m.pending.tools[0].Cancelled {
		t.Error("Cancelled flag not set")
	}
}
