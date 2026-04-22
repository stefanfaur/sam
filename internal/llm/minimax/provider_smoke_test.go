//go:build smoke

package minimax

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/llm"
)

func TestSmoke(t *testing.T) {
	apiKey := os.Getenv("MINIMAX_API_KEY")
	if apiKey == "" {
		t.Skip("MINIMAX_API_KEY not set")
	}

	prov, err := New(Options{})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	req := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Say 'ok' in exactly one word"}}},
		},
		MaxTokens: 10,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := prov.Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var textDeltas []string
	var stopReason string

	for ev := range events {
		switch ev.Type {
		case llm.EventTextDelta:
			textDeltas = append(textDeltas, ev.Text)
		case llm.EventMessageStop:
			stopReason = ev.StopReason
		case llm.EventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}

	if len(textDeltas) == 0 {
		t.Error("no text deltas received")
	}

	if stopReason == "" {
		t.Error("no message stop received")
	}
}
