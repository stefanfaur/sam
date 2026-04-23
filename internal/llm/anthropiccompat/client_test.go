package anthropiccompat

import (
	"encoding/json"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
)

func TestBuildToolsPreservesExplicitRequired(t *testing.T) {
	tools := []llm.ToolDef{{
		Name: "f",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "string"},
				"b": map[string]any{"type": "string"},
			},
			"required": []any{"a"},
		},
	}}
	out := BuildTools(tools)
	if len(out) != 1 {
		t.Fatalf("got %d tools", len(out))
	}
	// Marshal to JSON to inspect what the SDK would send.
	raw, err := json.Marshal(out[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	schema, ok := doc["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("input_schema missing in %s", raw)
	}
	req, ok := schema["required"].([]any)
	if !ok || len(req) != 1 || req[0] != "a" {
		t.Errorf("required not preserved: %v", schema["required"])
	}
}

func TestBuildToolsEmptyRequiredWhenAbsent(t *testing.T) {
	tools := []llm.ToolDef{{
		Name: "f",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"a": map[string]any{"type": "string"}},
		},
	}}
	out := BuildTools(tools)
	raw, _ := json.Marshal(out[0])
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	schema, _ := doc["input_schema"].(map[string]any)
	if r, has := schema["required"]; has {
		if arr, ok := r.([]any); ok && len(arr) != 0 {
			t.Errorf("required synthesized unexpectedly: %v", r)
		}
	}
}

func TestBuildMessagesDropsContentThinking(t *testing.T) {
	msgs := []llm.Message{{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{Type: llm.ContentThinking, Text: "private chain-of-thought"},
			{Type: llm.ContentText, Text: "answer"},
		},
	}}
	out := BuildMessages(msgs)
	raw, _ := json.Marshal(out)
	s := string(raw)
	if contains(s, "private chain-of-thought") {
		t.Errorf("thinking leaked into wire: %s", s)
	}
	if !contains(s, "answer") {
		t.Errorf("text missing: %s", s)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && indexOf(s, sub) >= 0
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
