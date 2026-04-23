package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestSingleTextResponse(t *testing.T) {
	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "Hello"},
		{Type: llm.EventTextDelta, Text: " world"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})

	reg := tools.NewRegistry()
	agent := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
	})
	agent.Start()
	defer agent.Close()

	events, err := collectEvents(agent.Submit(context.Background(), "hi"))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Should have TextDelta events
	var textDeltas []string
	for _, e := range events {
		if td, ok := e.(TextDelta); ok {
			textDeltas = append(textDeltas, td.Text)
		}
	}

	if len(textDeltas) != 2 {
		t.Errorf("expected 2 text deltas, got %d: %v", len(textDeltas), textDeltas)
	}

	expected := "Hello world"
	got := joinText(textDeltas)
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}

	// History should have user message + assistant message
	if len(agent.history) != 2 {
		t.Errorf("expected 2 messages in history, got %d", len(agent.history))
	}
}

func TestOneToolTurn(t *testing.T) {
	// First call: tool use
	// Second call: final text
	prov := fake.New(
		[]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventTextDelta, Text: "Let me read that file."},
			{Type: llm.EventToolUseStart, ToolUseID: "tool_1", ToolName: "Read"},
			{Type: llm.EventToolUseDelta, ToolUseID: "tool_1", PartialJSON: `{"file_path":"/tmp/test.go"}`},
			{Type: llm.EventToolUseStop, ToolUseID: "tool_1"},
			{Type: llm.EventMessageStop, StopReason: "tool_use"},
		},
		[]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventTextDelta, Text: "The file contains Go code."},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		},
	)

	tracker := tools.NewReadTracker()
	reg := tools.NewRegistry()
	reg.Register(tools.NewRead(tracker))

	agent := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
	})
	agent.Start()
	defer agent.Close()

	events, err := collectEvents(agent.Submit(context.Background(), "read the file"))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Check for ToolCall event
	var toolCalls []ToolCall
	for _, e := range events {
		if tc, ok := e.(ToolCall); ok {
			toolCalls = append(toolCalls, tc)
		}
	}

	if len(toolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(toolCalls))
	}

	// Check for ToolResult event
	var toolResults []ToolResult
	for _, e := range events {
		if tr, ok := e.(ToolResult); ok {
			toolResults = append(toolResults, tr)
		}
	}

	if len(toolResults) != 1 {
		t.Errorf("expected 1 tool result, got %d", len(toolResults))
	}

	// Should end with TurnDone
	var turnDone bool
	for _, e := range events {
		if _, ok := e.(TurnDone); ok {
			turnDone = true
		}
	}
	if !turnDone {
		t.Error("expected TurnDone event")
	}

	// History: user → assistant(text+tool_use) → user(tool_result) → assistant(text)
	if len(agent.history) != 4 {
		t.Errorf("expected 4 messages in history, got %d", len(agent.history))
	}
}

func TestContextCancelMidStream(t *testing.T) {
	// Provider that hangs (no message_stop) - use a long script to simulate hang
	hangScript := []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "Hello"},
		{Type: llm.EventTextDelta, Text: " world"},
		// No message_stop - this will cause stream to "hang"
	}

	prov := fake.New(hangScript)

	reg := tools.NewRegistry()
	agent := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
	})
	agent.Start()
	defer agent.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	eventsCh := agent.Submit(ctx, "hi")

	events, err := collectEvents(eventsCh)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Should have some text deltas before timeout
	var textDeltas []string
	for _, e := range events {
		if td, ok := e.(TextDelta); ok {
			textDeltas = append(textDeltas, td.Text)
		}
	}

	if len(textDeltas) < 1 {
		t.Error("expected at least one text delta before timeout")
	}

	// Should have error event (from context deadline)
	var hasError bool
	for _, e := range events {
		if _, ok := e.(ErrorEvent); ok {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected ErrorEvent after timeout")
	}
}

func TestMaxIterationsCap(t *testing.T) {
	// Provider that always returns tool_use
	toolUseScript := []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: "tool_1", ToolName: "Read"},
		{Type: llm.EventToolUseDelta, ToolUseID: "tool_1", PartialJSON: `{"file_path":"/tmp/a"}`},
		{Type: llm.EventToolUseStop, ToolUseID: "tool_1"},
		{Type: llm.EventMessageStop, StopReason: "tool_use"},
	}

	prov := fake.New(toolUseScript, toolUseScript, toolUseScript, toolUseScript)

	tracker := tools.NewReadTracker()
	reg := tools.NewRegistry()
	reg.Register(tools.NewRead(tracker))

	agent := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
		MaxIters: 3, // Low for testing
	})
	agent.Start()
	defer agent.Close()

	events, err := collectEvents(agent.Submit(context.Background(), "do something"))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Should get error after max iterations
	var hasMaxIterError bool
	for _, e := range events {
		if errEv, ok := e.(ErrorEvent); ok {
			if errEv.Err != nil && errEv.Err.Error() == "agent: max iterations (3) reached" {
				hasMaxIterError = true
			}
		}
	}

	if !hasMaxIterError {
		t.Error("expected max iterations error")
	}
}

func TestThinkingDeltaAccumulatesIntoContentBlock(t *testing.T) {
	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventThinkingDelta, Text: "step one, "},
		{Type: llm.EventThinkingDelta, Text: "step two."},
		{Type: llm.EventTextDelta, Text: "answer"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})
	reg := tools.NewRegistry()
	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()

	if _, err := collectEvents(agent.Submit(context.Background(), "hi")); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(agent.history) != 2 {
		t.Fatalf("history len: %d", len(agent.history))
	}
	asst := agent.history[1]
	var thinking []llm.ContentBlock
	var text []llm.ContentBlock
	for _, b := range asst.Content {
		switch b.Type {
		case llm.ContentThinking:
			thinking = append(thinking, b)
		case llm.ContentText:
			text = append(text, b)
		}
	}
	if len(thinking) != 1 {
		t.Fatalf("expected 1 collapsed thinking block, got %d", len(thinking))
	}
	if thinking[0].Text != "step one, step two." {
		t.Errorf("thinking text: %q", thinking[0].Text)
	}
	if len(text) != 1 || text[0].Text != "answer" {
		t.Errorf("text blocks: %+v", text)
	}
	// Ordering: thinking must appear before text.
	if asst.Content[0].Type != llm.ContentThinking || asst.Content[1].Type != llm.ContentText {
		t.Errorf("bad ordering: %+v", asst.Content)
	}
}

func TestMultiTurnReasoningPreservation(t *testing.T) {
	// Turn 1: emit thinking + tool_use + stop tool_use (forces a second turn).
	// Turn 2: emit a simple end-of-turn.
	prov := fake.New(
		[]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventThinkingDelta, Text: "reasoning body"},
			{Type: llm.EventTextDelta, Text: "text body"},
			{Type: llm.EventToolUseStart, ToolUseID: "t1", ToolName: "Read"},
			{Type: llm.EventToolUseDelta, ToolUseID: "t1", PartialJSON: `{"file_path":"/x"}`},
			{Type: llm.EventToolUseStop, ToolUseID: "t1"},
			{Type: llm.EventMessageStop, StopReason: "tool_use"},
		},
		[]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventTextDelta, Text: "done"},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		},
	)
	tracker := tools.NewReadTracker()
	reg := tools.NewRegistry()
	reg.Register(tools.NewRead(tracker))

	agent := New(Options{Provider: prov, Tools: reg, Policy: policy.AllowAll()})
	agent.Start()
	defer agent.Close()
	if _, err := collectEvents(agent.Submit(context.Background(), "hi")); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if len(prov.Calls) < 2 {
		t.Fatalf("expected ≥2 provider calls, got %d", len(prov.Calls))
	}
	// On turn 2 the agent must send the prior assistant message with a
	// ContentThinking block preserved.
	secondReq := prov.Calls[1]
	var found *llm.ContentBlock
	var sawOrderThinkingFirst bool
	for _, msg := range secondReq.Messages {
		if msg.Role != llm.RoleAssistant {
			continue
		}
		for i, b := range msg.Content {
			if b.Type == llm.ContentThinking && b.Text == "reasoning body" {
				found = &msg.Content[i]
				// Thinking should precede the text/tool_use blocks
				// (mirroring stream-event arrival order).
				if i == 0 {
					sawOrderThinkingFirst = true
				}
			}
		}
	}
	if found == nil {
		t.Fatalf("ContentThinking with prior reasoning not found in turn-2 request: %+v", secondReq.Messages)
	}
	if !sawOrderThinkingFirst {
		t.Error("thinking block not in leading position on assistant history")
	}
}

// Helper functions

func collectEvents(ch <-chan Event) ([]Event, error) {
	var events []Event
	for e := range ch {
		events = append(events, e)
	}
	return events, nil
}

func joinText(deltas []string) string {
	result := ""
	for _, d := range deltas {
		result += d
	}
	return result
}
