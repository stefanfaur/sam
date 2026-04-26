package openaicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
)

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestToWireMessagesUserTextOnly(t *testing.T) {
	msgs := []llm.Message{{
		Role:    llm.RoleUser,
		Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "hello"}},
	}}
	caps := DefaultCaps("gpt-4o")
	got := toWireMessages("", msgs, caps)
	if len(got) != 1 || got[0].Role != "user" || got[0].Content == nil || *got[0].Content != "hello" {
		t.Fatalf("bad: %+v", got)
	}
}

func TestToWireMessagesToolResultsSuccessAndError(t *testing.T) {
	msgs := []llm.Message{{
		Role: llm.RoleUser,
		Content: []llm.ContentBlock{
			{Type: llm.ContentToolResult, ToolUseID: "id1", Output: "ok"},
			{Type: llm.ContentToolResult, ToolUseID: "id2", Output: "boom", IsError: true},
		},
	}}
	got := toWireMessages("", msgs, DefaultCaps("gpt-4o"))
	if len(got) != 2 {
		t.Fatalf("want 2 tool msgs, got %d", len(got))
	}
	if got[0].Role != "tool" || got[0].ToolCallID != "id1" || *got[0].Content != "ok" {
		t.Errorf("success: %+v", got[0])
	}
	if got[1].Role != "tool" || got[1].ToolCallID != "id2" {
		t.Errorf("error role/id: %+v", got[1])
	}
	var payload struct {
		Error  bool   `json:"error"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(*got[1].Content), &payload); err != nil {
		t.Fatalf("error payload: %v", err)
	}
	if !payload.Error || payload.Output != "boom" {
		t.Errorf("error payload mismatch: %+v", payload)
	}
}

func TestToWireMessagesAssistantTextPlusToolUse(t *testing.T) {
	msgs := []llm.Message{{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{Type: llm.ContentText, Text: "let me try"},
			{Type: llm.ContentToolUse, ToolUseID: "call_1", ToolName: "Read",
				Input: json.RawMessage(`{"file_path":"/x"}`)},
		},
	}}
	got := toWireMessages("", msgs, DefaultCaps("gpt-4o"))
	if len(got) != 1 {
		t.Fatalf("want 1 msg, got %d", len(got))
	}
	m := got[0]
	if m.Role != "assistant" || m.Content == nil || *m.Content != "let me try" {
		t.Errorf("content: %+v", m)
	}
	if len(m.ToolCalls) != 1 || m.ToolCalls[0].ID != "call_1" ||
		m.ToolCalls[0].Function.Name != "Read" ||
		m.ToolCalls[0].Function.Arguments != `{"file_path":"/x"}` {
		t.Errorf("tool_calls: %+v", m.ToolCalls)
	}
}

func TestAssistantThinkingPlusToolUseNoTextContentOmitted(t *testing.T) {
	caps := DefaultCaps("trinity-large-thinking")
	msgs := []llm.Message{{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{Type: llm.ContentThinking, Text: "must call tool"},
			{Type: llm.ContentToolUse, ToolUseID: "t1", ToolName: "Read",
				Input: json.RawMessage(`{}`)},
		},
	}}
	got := toWireMessages("", msgs, caps)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	m := got[0]
	// Tool-call-only assistant messages now emit `"content":""` (explicit empty
	// string) so Trinity / other strict OpenAI-compat servers don't reject the
	// turn for null content. Safe on all tested servers.
	if m.Content == nil || *m.Content != "" {
		t.Errorf("content should be empty string, got %v", m.Content)
	}
	if m.ReasoningContent != "must call tool" {
		t.Errorf("reasoning_content: %q", m.ReasoningContent)
	}
	if m.Reasoning != "" {
		t.Errorf("reasoning should be empty when source is reasoning_content, got %q", m.Reasoning)
	}
	raw := mustMarshal(t, m)
	if !strings.Contains(raw, "\"content\":\"\"") {
		t.Errorf("expected explicit empty content in JSON, got: %s", raw)
	}
}

func TestAssistantThinkingDroppedWhenEchoOff(t *testing.T) {
	caps := DefaultCaps("gpt-4o") // EchoReasoning=false
	msgs := []llm.Message{{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{Type: llm.ContentThinking, Text: "secret chain-of-thought"},
			{Type: llm.ContentText, Text: "answer"},
		},
	}}
	got := toWireMessages("", msgs, caps)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Reasoning != "" || got[0].ReasoningContent != "" {
		t.Errorf("reasoning should be dropped: r=%q rc=%q", got[0].Reasoning, got[0].ReasoningContent)
	}
}

func TestAssistantThinkingRoutesByReasoningSource(t *testing.T) {
	mk := func(model string) chatMessage {
		caps := DefaultCaps(model)
		msgs := []llm.Message{{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.ContentThinking, Text: "think"},
				{Type: llm.ContentText, Text: "hi"},
			},
		}}
		got := toWireMessages("", msgs, caps)
		if len(got) != 1 {
			t.Fatalf("got %d", len(got))
		}
		return got[0]
	}
	// kimi-k2 uses reasoning_content on the wire.
	k := mk("kimi-k2.6")
	if k.ReasoningContent != "think" || k.Reasoning != "" {
		t.Errorf("kimi routing: r=%q rc=%q", k.Reasoning, k.ReasoningContent)
	}
	// deepseek-r also uses reasoning_content.
	d := mk("deepseek-r1")
	if d.ReasoningContent != "think" || d.Reasoning != "" {
		t.Errorf("deepseek routing: r=%q rc=%q", d.Reasoning, d.ReasoningContent)
	}
}

func TestToWireToolsPreservesRequired(t *testing.T) {
	tools := []llm.ToolDef{{
		Name:        "f",
		Description: "d",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"a": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"}},
			"required":   []any{"a"},
		},
	}}
	got := toWireTools(tools)
	req, ok := got[0].Function.Parameters["required"].([]any)
	if !ok || len(req) != 1 || req[0] != "a" {
		t.Errorf("required not preserved: %+v", got[0].Function.Parameters)
	}
}

func TestToWireToolsOmitsRequiredWhenAbsent(t *testing.T) {
	tools := []llm.ToolDef{{
		Name:   "f",
		Schema: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}},
	}}
	got := toWireTools(tools)
	if _, has := got[0].Function.Parameters["required"]; has {
		t.Errorf("required synthesized unexpectedly: %+v", got[0].Function.Parameters)
	}
}

func TestBuildRequestNoTools(t *testing.T) {
	r := buildRequest(llm.Request{Model: "gpt-4o-mini"}, DefaultCaps("gpt-4o-mini"), "")
	raw := mustMarshal(t, r)
	if strings.Contains(raw, "\"tools\"") || strings.Contains(raw, "\"tool_choice\"") {
		t.Errorf("tools/tool_choice leaked into empty-tools request: %s", raw)
	}
}

func TestBuildRequestSystemRoleBranches(t *testing.T) {
	sys := "you are X"
	msgs := []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "hi"}}}}

	sysReq := buildRequest(llm.Request{Model: "gpt-4o-mini", System: sys, Messages: msgs}, DefaultCaps("gpt-4o-mini"), "")
	if sysReq.Messages[0].Role != "system" {
		t.Errorf("gpt-4o system role: %q", sysReq.Messages[0].Role)
	}
	devReq := buildRequest(llm.Request{Model: "gpt-5", System: sys, Messages: msgs}, DefaultCaps("gpt-5"), "")
	if devReq.Messages[0].Role != "developer" {
		t.Errorf("gpt-5 developer role: %q", devReq.Messages[0].Role)
	}
}

func TestBuildRequestReasoningEffort(t *testing.T) {
	r := buildRequest(llm.Request{Model: "gpt-5"}, DefaultCaps("gpt-5"), "high")
	if r.ReasoningEffort != "high" {
		t.Errorf("effort: %q", r.ReasoningEffort)
	}
	// Unsupported caps → field stays empty.
	r2 := buildRequest(llm.Request{Model: "gpt-4o"}, DefaultCaps("gpt-4o"), "high")
	if r2.ReasoningEffort != "" {
		t.Errorf("effort leaked on non-reasoning model: %q", r2.ReasoningEffort)
	}
}

func TestBuildRequestMaxTokensField(t *testing.T) {
	r4 := buildRequest(llm.Request{Model: "gpt-4o", MaxTokens: 1024}, DefaultCaps("gpt-4o"), "")
	if r4.MaxTokens == nil || *r4.MaxTokens != 1024 || r4.MaxCompletionTokens != nil {
		t.Errorf("gpt-4o max: %+v", r4)
	}
	r5 := buildRequest(llm.Request{Model: "gpt-5", MaxTokens: 2048}, DefaultCaps("gpt-5"), "")
	if r5.MaxCompletionTokens == nil || *r5.MaxCompletionTokens != 2048 || r5.MaxTokens != nil {
		t.Errorf("gpt-5 max: %+v", r5)
	}
}

func TestToWireMessages_FormattingPrepend(t *testing.T) {
	caps := Capabilities{SystemRole: "developer", PrependFormatting: true}
	out := toWireMessages("You are SAM.", nil, caps)
	if len(out) == 0 || out[0].Content == nil {
		t.Fatalf("expected system message, got: %+v", out)
	}
	want := "Formatting re-enabled.\nYou are SAM."
	if got := *out[0].Content; got != want {
		t.Errorf("system content: got %q, want %q", got, want)
	}
	caps.PrependFormatting = false
	out = toWireMessages("You are SAM.", nil, caps)
	if got := *out[0].Content; got != "You are SAM." {
		t.Errorf("no-prepend case: got %q", got)
	}
}

func TestToWireMessages_SystemAsUserSplice(t *testing.T) {
	caps := Capabilities{SystemRole: "user"}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "hi"}}},
	}
	out := toWireMessages("You are SAM.", msgs, caps)
	for _, m := range out {
		if m.Role == "system" {
			t.Errorf("unexpected system message in wire output: %+v", m)
		}
	}
	if len(out) == 0 || out[0].Role != "user" || out[0].Content == nil {
		t.Fatalf("expected first user message, got: %+v", out)
	}
	want := "<system>\nYou are SAM.\n</system>\n\nhi"
	if got := *out[0].Content; got != want {
		t.Errorf("spliced content: got %q, want %q", got, want)
	}
}

func TestToWireMessages_SystemAsUser_NoUserMessage(t *testing.T) {
	caps := Capabilities{SystemRole: "user"}
	out := toWireMessages("You are SAM.", nil, caps)
	if len(out) != 1 || out[0].Role != "user" || out[0].Content == nil {
		t.Fatalf("expected one synthesized user message, got: %+v", out)
	}
	want := "<system>\nYou are SAM.\n</system>"
	if got := *out[0].Content; got != want {
		t.Errorf("synthesized content: got %q, want %q", got, want)
	}
}

func TestTranslateAssistantMessage_ToolCallOnlyContentEmpty(t *testing.T) {
	msg := llm.Message{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{Type: llm.ContentToolUse, ToolUseID: "t1", ToolName: "read", Input: json.RawMessage(`{"path":"x"}`)},
		},
	}
	got, ok := translateAssistantMessage(msg, Capabilities{})
	if !ok {
		t.Fatalf("message dropped")
	}
	if got.Content == nil {
		t.Errorf("Content is nil; expected pointer to empty string")
	} else if *got.Content != "" {
		t.Errorf("Content = %q; want \"\"", *got.Content)
	}
	if len(got.ToolCalls) != 1 {
		t.Errorf("ToolCalls = %+v; want 1 entry", got.ToolCalls)
	}
}

func TestDefaultCaps_PrependFormattingOnReasoningModels(t *testing.T) {
	on := []string{"gpt-5", "gpt-5-mini", "o1-preview", "o3-mini", "o4-mini"}
	off := []string{"gpt-4o", "gpt-4.1-mini", "kimi-k2", "trinity-large", "deepseek-r1", "deepseek-chat"}
	for _, m := range on {
		if got := DefaultCaps(m); !got.PrependFormatting {
			t.Errorf("DefaultCaps(%q).PrependFormatting = false; want true", m)
		}
	}
	for _, m := range off {
		if got := DefaultCaps(m); got.PrependFormatting {
			t.Errorf("DefaultCaps(%q).PrependFormatting = true; want false", m)
		}
	}
}

func TestBuildRequestParallelToolCallsOnlyWhenForcingFalse(t *testing.T) {
	caps := DefaultCaps("gpt-4o")
	r := buildRequest(llm.Request{Model: "gpt-4o"}, caps, "")
	if r.ParallelToolCalls != nil {
		t.Errorf("parallel_tool_calls should be omitted on happy path")
	}
	caps.SupportsParallelToolCalls = false
	r2 := buildRequest(llm.Request{Model: "gpt-4o"}, caps, "")
	if r2.ParallelToolCalls == nil || *r2.ParallelToolCalls {
		t.Errorf("want false, got %+v", r2.ParallelToolCalls)
	}
}
