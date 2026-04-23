package openaicompat

import (
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
)

func sendAll(chunks []streamChunk) <-chan streamChunk {
	ch := make(chan streamChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func collect(ch <-chan llm.StreamEvent) []llm.StreamEvent {
	var out []llm.StreamEvent
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func ptr(s string) *string { return &s }

func TestStreamPlainText(t *testing.T) {
	stop := "stop"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("Hello ")}}}},
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("world")}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
		{Usage: &streamUsage{PromptTokens: 10, CompletionTokens: 3}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	want := []llm.StreamEventType{
		llm.EventMessageStart,
		llm.EventTextDelta,
		llm.EventTextDelta,
		llm.EventMessageStop,
	}
	if len(evs) != len(want) {
		t.Fatalf("len %d, want %d: %+v", len(evs), len(want), evs)
	}
	for i, ty := range want {
		if evs[i].Type != ty {
			t.Errorf("ev %d: %s want %s", i, evs[i].Type, ty)
		}
	}
	if evs[3].StopReason != "end_turn" || evs[3].InputTokens != 10 || evs[3].OutputTokens != 3 {
		t.Errorf("stop: %+v", evs[3])
	}
}

func TestStreamUsageCachedTokens(t *testing.T) {
	stop := "stop"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("hi")}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
		{Usage: &streamUsage{
			PromptTokens:        5019,
			CompletionTokens:    20,
			PromptTokensDetails: streamUsagePromptDet{CachedTokens: 5008},
		}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("trinity-large-thinking"), nil))
	last := evs[len(evs)-1]
	if last.Type != llm.EventMessageStop {
		t.Fatalf("last type %s", last.Type)
	}
	if last.InputTokens != 5019 || last.OutputTokens != 20 || last.CacheReadInput != 5008 {
		t.Errorf("usage: in=%d out=%d cache=%d", last.InputTokens, last.OutputTokens, last.CacheReadInput)
	}
}

func TestStreamReasoningContent(t *testing.T) {
	stop := "stop"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{ReasoningContent: "thinking…"}}}},
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("answer")}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("deepseek-r1"), nil))
	types := []llm.StreamEventType{}
	for _, e := range evs {
		types = append(types, e.Type)
	}
	want := []llm.StreamEventType{llm.EventMessageStart, llm.EventThinkingDelta, llm.EventTextDelta, llm.EventMessageStop}
	for i, w := range want {
		if i >= len(types) || types[i] != w {
			t.Errorf("types[%d]: got %v want %v (all=%v)", i, types, want, types)
		}
	}
}

func TestStreamReasoningField(t *testing.T) {
	stop := "stop"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Reasoning: "X"}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("trinity-large-thinking"), nil))
	// MessageStart, ThinkingDelta("X"), MessageStop
	if len(evs) != 3 || evs[1].Type != llm.EventThinkingDelta || evs[1].Text != "X" {
		t.Errorf("unexpected: %+v", evs)
	}
}

func TestStreamToolCallsParallel(t *testing.T) {
	stop := "tool_calls"
	chunks := []streamChunk{
		// index 0 start
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 0, ID: "a", Function: streamToolCallDeltaFunc{Name: "Read", Arguments: `{"file":`},
		}}}}}},
		// index 1 start interleaved
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 1, ID: "b", Function: streamToolCallDeltaFunc{Name: "Bash", Arguments: `{"cmd":`},
		}}}}}},
		// index 0 continues
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 0, Function: streamToolCallDeltaFunc{Arguments: `"/x"}`},
		}}}}}},
		// index 1 continues
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 1, Function: streamToolCallDeltaFunc{Arguments: `"ls"}`},
		}}}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	// Expect: Start, ToolStart(a), ToolDelta, ToolStart(b), ToolDelta, ToolDelta, ToolDelta,
	// ToolStop(a), ToolStop(b), MessageStop
	var starts, stops []string
	for _, e := range evs {
		switch e.Type {
		case llm.EventToolUseStart:
			starts = append(starts, e.ToolUseID+":"+e.ToolName)
		case llm.EventToolUseStop:
			stops = append(stops, e.ToolUseID)
		}
	}
	if len(starts) != 2 || starts[0] != "a:Read" || starts[1] != "b:Bash" {
		t.Errorf("starts: %v", starts)
	}
	if len(stops) != 2 || stops[0] != "a" || stops[1] != "b" {
		t.Errorf("stops (must be in registration order): %v", stops)
	}
	last := evs[len(evs)-1]
	if last.Type != llm.EventMessageStop || last.StopReason != "tool_use" {
		t.Errorf("final stop: %+v", last)
	}
}

func TestStreamRefusal(t *testing.T) {
	ref := "I cannot help with that."
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Refusal: &ref}}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	// MessageStart, EventError — no MessageStop.
	if len(evs) != 2 || evs[1].Type != llm.EventError {
		t.Fatalf("evs: %+v", evs)
	}
}

func TestStreamFinishCoWithContent(t *testing.T) {
	stop := "stop"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("last")}, FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	// Start, TextDelta, MessageStop
	if len(evs) != 3 || evs[1].Type != llm.EventTextDelta || evs[2].Type != llm.EventMessageStop {
		t.Fatalf("evs: %+v", evs)
	}
	if evs[2].StopReason != "end_turn" {
		t.Errorf("stop reason: %q", evs[2].StopReason)
	}
}

func TestStreamMissingFinishReason(t *testing.T) {
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("hi")}}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	last := evs[len(evs)-1]
	if last.Type != llm.EventMessageStop || last.StopReason != "end_turn" {
		t.Errorf("clean close: %+v", last)
	}
}

func TestStreamToolIDOnlyOnFirstChunk(t *testing.T) {
	stop := "tool_calls"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 0, ID: "x", Function: streamToolCallDeltaFunc{Name: "T"},
		}}}}}},
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 0, Function: streamToolCallDeltaFunc{Arguments: `{"k":1}`},
		}}}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	var start, stopEv *llm.StreamEvent
	for i, e := range evs {
		if e.Type == llm.EventToolUseStart {
			start = &evs[i]
		}
		if e.Type == llm.EventToolUseStop {
			stopEv = &evs[i]
		}
	}
	if start == nil || start.ToolUseID != "x" || start.ToolName != "T" {
		t.Errorf("start: %+v", start)
	}
	if stopEv == nil || stopEv.ToolUseID != "x" {
		t.Errorf("stop: %+v", stopEv)
	}
}

func TestStreamInvalidToolJSONSurfacesError(t *testing.T) {
	stop := "tool_calls"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 0, ID: "x", Function: streamToolCallDeltaFunc{Name: "T", Arguments: `{"k":`},
		}}}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("gpt-4o"), nil))
	// Must contain EventError and NOT EventToolUseStop or MessageStop (we abort).
	var sawErr, sawStop bool
	for _, e := range evs {
		if e.Type == llm.EventError {
			sawErr = true
		}
		if e.Type == llm.EventToolUseStop || e.Type == llm.EventMessageStop {
			sawStop = true
		}
	}
	if !sawErr {
		t.Error("expected EventError")
	}
	if sawStop {
		t.Error("must not emit Tool/Message stop on invalid args")
	}
}

func TestStreamThinkingPlusToolNoContent(t *testing.T) {
	stop := "tool_calls"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{ReasoningContent: "plan"}}}},
		{Choices: []streamChoice{{Delta: streamDelta{ToolCalls: []streamToolCallDelta{{
			Index: 0, ID: "x", Function: streamToolCallDeltaFunc{Name: "T", Arguments: `{}`},
		}}}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	evs := collect(translate(sendAll(chunks), DefaultCaps("deepseek-r1"), nil))
	var hasText, hasThink bool
	for _, e := range evs {
		if e.Type == llm.EventTextDelta {
			hasText = true
		}
		if e.Type == llm.EventThinkingDelta {
			hasThink = true
		}
	}
	if hasText {
		t.Error("no text expected")
	}
	if !hasThink {
		t.Error("thinking event missing")
	}
}

func TestStreamThinkTagParser(t *testing.T) {
	stop := "stop"
	chunks := []streamChunk{
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("hi <think>")}}}},
		{Choices: []streamChoice{{Delta: streamDelta{Content: ptr("plan</think> bye")}}}},
		{Choices: []streamChoice{{FinishReason: &stop}}},
	}
	p := NewThinkTagParser()
	evs := collect(translate(sendAll(chunks), DefaultCaps("trinity-large-thinking"), p))
	var text, think string
	for _, e := range evs {
		switch e.Type {
		case llm.EventTextDelta:
			text += e.Text
		case llm.EventThinkingDelta:
			think += e.Text
		}
	}
	if text != "hi  bye" || think != "plan" {
		t.Errorf("text=%q think=%q", text, think)
	}
}
