//go:build smoke

package minimax_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/minimax"
)

func TestMiniMaxToolCall(t *testing.T) {
	p, err := minimax.New(minimax.Options{})
	if err != nil {
		t.Fatalf("provider error: %v", err)
	}

	req := llm.Request{
		Model:    "MiniMax-M2.7",
		System:   "You are a helpful assistant.",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Read the file /tmp/test.txt and tell me what it contains."}}}},
		Tools: []llm.ToolDef{{
			Name:        "Read",
			Description: "Read file contents",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string"},
				},
				"required": []any{"file_path"},
			},
		}},
		MaxTokens: 1024,
	}

	ctx := context.Background()
	events, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	fmt.Println("Events:")
	hasToolUse := false
	for ev := range events {
		switch ev.Type {
		case llm.EventMessageStart:
			fmt.Println("  [message_start]")
		case llm.EventContentBlockStart:
			fmt.Println("  [content_block_start]")
		case llm.EventTextDelta:
			fmt.Printf("  [text_delta]: %q\n", ev.Text)
		case llm.EventToolUseStart:
			hasToolUse = true
			fmt.Printf("  [tool_use_start]: id=%s name=%s\n", ev.ToolUseID, ev.ToolName)
		case llm.EventToolUseDelta:
			fmt.Printf("  [tool_use_delta]: partial=%q\n", ev.PartialJSON)
		case llm.EventToolUseStop:
			fmt.Println("  [tool_use_stop]")
		case llm.EventMessageStop:
			fmt.Printf("  [message_stop]: reason=%s\n", ev.StopReason)
		case llm.EventError:
			fmt.Printf("  [error]: %v\n", ev.Err)
		}
	}

	if !hasToolUse {
		t.Log("NOTE: No tool_use events received (model may not have called tool)")
	}
}
