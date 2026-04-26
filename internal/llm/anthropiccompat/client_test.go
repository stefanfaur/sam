package anthropiccompat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestBuildToolsPropertiesFlat(t *testing.T) {
	tools := []llm.ToolDef{{
		Name: "Read",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
			},
			"required": []any{"file_path"},
		},
	}}
	out := BuildTools(tools)
	raw, _ := json.Marshal(out[0])
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	schema, _ := doc["input_schema"].(map[string]any)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing: %s", raw)
	}
	if _, leaked := props["type"]; leaked {
		t.Errorf("nested 'type' leaked into properties: %s", raw)
	}
	if _, leaked := props["required"]; leaked {
		t.Errorf("nested 'required' leaked into properties (trips strict validators): %s", raw)
	}
	if _, leaked := props["properties"]; leaked {
		t.Errorf("nested 'properties' leaked into properties: %s", raw)
	}
	fp, ok := props["file_path"].(map[string]any)
	if !ok {
		t.Fatalf("file_path property missing: %s", raw)
	}
	if fp["type"] != "string" {
		t.Errorf("file_path type: %v", fp["type"])
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

func TestBuildMessagesDropsUnsignedThinking(t *testing.T) {
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
		t.Errorf("unsigned thinking leaked into wire: %s", s)
	}
	if !contains(s, "answer") {
		t.Errorf("text missing: %s", s)
	}
}

func TestBuildMessagesSkipsOnlyUnsignedThinking(t *testing.T) {
	// Previous bug: an assistant message whose only content was an
	// unsigned thinking block got filtered to zero blocks, producing an
	// empty-content assistant message on the wire. DeepSeek rejects
	// that with `messages.N: all messages must have non-empty content`.
	msgs := []llm.Message{
		{
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "hi"}},
		},
		{
			Role:    llm.RoleAssistant,
			Content: []llm.ContentBlock{{Type: llm.ContentThinking, Text: "x"}}, // unsigned
		},
		{
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "follow up"}},
		},
	}
	out := BuildMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 wire messages, got %d", len(out))
	}
}

func TestBuildMessagesRoundTripsSignedThinking(t *testing.T) {
	msgs := []llm.Message{{
		Role: llm.RoleAssistant,
		Content: []llm.ContentBlock{
			{Type: llm.ContentThinking, Text: "signed reasoning", Signature: "sig-abc-123"},
			{Type: llm.ContentText, Text: "answer"},
		},
	}}
	out := BuildMessages(msgs)
	raw, _ := json.Marshal(out)
	s := string(raw)
	if !contains(s, "signed reasoning") {
		t.Errorf("signed thinking not round-tripped: %s", s)
	}
	if !contains(s, "sig-abc-123") {
		t.Errorf("signature not round-tripped: %s", s)
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

// bodyCapturingServer returns an httptest.Server that records the first
// request body and replies with 400 so the SDK short-circuits without
// retrying (retries apply to 5xx/408/429, not 400).
func bodyCapturingServer(t *testing.T) (*httptest.Server, func() []byte) {
	t.Helper()
	var mu sync.Mutex
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		if captured == nil {
			captured = b
		}
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"test"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []byte {
		mu.Lock()
		defer mu.Unlock()
		return captured
	}
}

func TestAnthropicCompat_MaxTokensAndThinkingInBody(t *testing.T) {
	srv, body := bodyCapturingServer(t)
	c, err := New(Options{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, _ = c.Stream(context.Background(), llm.Request{
		Model:                "deepseek-v4-pro",
		MaxTokens:            192_000,
		ThinkingBudgetTokens: 120_000,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "hi"}},
		}},
	})
	raw := body()
	if len(raw) == 0 {
		t.Fatal("no request body captured")
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, raw)
	}
	if got, _ := doc["max_tokens"].(float64); got != 192_000 {
		t.Errorf("max_tokens: got %v want 192000 — body=%s", doc["max_tokens"], raw)
	}
	th, ok := doc["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking block missing: %s", raw)
	}
	if th["type"] != "enabled" {
		t.Errorf("thinking.type: %v want enabled", th["type"])
	}
	if bt, _ := th["budget_tokens"].(float64); bt != 120_000 {
		t.Errorf("thinking.budget_tokens: got %v want 120000", th["budget_tokens"])
	}
}

func TestAnthropicCompat_ThinkingBudgetZeroOmitsBlock(t *testing.T) {
	srv, body := bodyCapturingServer(t)
	c, err := New(Options{APIKey: "test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, _ = c.Stream(context.Background(), llm.Request{
		Model:     "deepseek-v4-pro",
		MaxTokens: 32_768,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "hi"}},
		}},
	})
	raw := body()
	if len(raw) == 0 {
		t.Fatal("no request body captured")
	}
	if strings.Contains(string(raw), `"thinking"`) {
		t.Errorf("thinking key must be absent when budget is zero: %s", raw)
	}
}
