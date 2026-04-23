//go:build integration

package openaicompat

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
)

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s not set; skipping live smoke", key)
	}
	return v
}

func TestProviderLiveOpenAI(t *testing.T) {
	requireEnv(t, "OPENAI_API_KEY")
	entry := config.ProviderEntry{
		Name:         "openai",
		Wire:         "openai",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyEnv:    "OPENAI_API_KEY",
		DefaultModel: "gpt-4o-mini",
	}
	p, err := NewProvider(entry, "gpt-4o-mini", nil)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, llm.Request{
		Model:     "gpt-4o-mini",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Say OK."}}}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var sawText, sawStop bool
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			sawText = true
		case llm.EventMessageStop:
			sawStop = true
		case llm.EventError:
			t.Fatalf("error: %v", ev.Err)
		}
	}
	if !sawText || !sawStop {
		t.Errorf("missing events: text=%v stop=%v", sawText, sawStop)
	}
}

func TestProviderLiveOpenAIReasoning(t *testing.T) {
	requireEnv(t, "OPENAI_API_KEY")
	entry := config.ProviderEntry{
		Name:         "openai",
		Wire:         "openai",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyEnv:    "OPENAI_API_KEY",
		DefaultModel: "gpt-5-mini",
	}
	effort := func(string) string { return "minimal" }
	p, err := NewProvider(entry, "gpt-5-mini", effort)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, llm.Request{
		Model:     "gpt-5-mini",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Reply with exactly: OK."}}}},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var outTokens int
	for ev := range ch {
		if ev.Type == llm.EventError {
			t.Fatalf("error: %v", ev.Err)
		}
		if ev.Type == llm.EventMessageStop {
			outTokens = ev.OutputTokens
		}
	}
	if outTokens == 0 {
		t.Errorf("expected non-zero output tokens")
	}
}

func TestProviderLiveArcee(t *testing.T) {
	requireEnv(t, "ARCEE_API_KEY")
	entry := config.ProviderEntry{
		Name:         "arcee",
		Wire:         "openai",
		BaseURL:      "https://conductor.arcee.ai/v1",
		APIKeyEnv:    "ARCEE_API_KEY",
		DefaultModel: "trinity-large-thinking",
	}
	p, err := NewProvider(entry, "trinity-large-thinking", nil)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, llm.Request{
		Model:     "trinity-large-thinking",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Think briefly then say OK."}}}},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var sawThink, sawText bool
	for ev := range ch {
		switch ev.Type {
		case llm.EventThinkingDelta:
			sawThink = true
		case llm.EventTextDelta:
			sawText = true
		case llm.EventError:
			t.Fatalf("error: %v", ev.Err)
		}
	}
	if !sawThink {
		t.Errorf("expected at least one ThinkingDelta (probe the wire shape if this fails)")
	}
	if !sawText {
		t.Errorf("expected at least one TextDelta")
	}
}
