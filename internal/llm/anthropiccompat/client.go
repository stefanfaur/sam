package anthropiccompat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/stefanfaur/sam/internal/llm"
)

var Debug = os.Getenv("SAM_DEBUG") == "1"

// BuildMessages projects cross-provider history into Anthropic SDK param
// messages. ContentThinking blocks with a signature are round-tripped via
// NewThinkingBlock (required for extended-thinking continuations on
// tool-use turns). Unsigned thinking drops silently.
func BuildMessages(in []llm.Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(in))
	for _, msg := range in {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		for _, block := range msg.Content {
			switch block.Type {
			case llm.ContentText:
				blocks = append(blocks, anthropic.NewTextBlock(block.Text))
			case llm.ContentThinking:
				if block.Signature == "" {
					continue
				}
				blocks = append(blocks, anthropic.NewThinkingBlock(block.Signature, block.Text))
			case llm.ContentToolUse:
				var input map[string]any
				if len(block.Input) > 0 {
					_ = json.Unmarshal(block.Input, &input)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(
					block.ToolUseID,
					input,
					block.ToolName,
				))
			case llm.ContentToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(
					block.ToolUseID,
					block.Output,
					block.IsError,
				))
			}
		}
		if msg.Role == llm.RoleAssistant {
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		} else {
			out = append(out, anthropic.NewUserMessage(blocks...))
		}
	}
	return out
}

// BuildTools projects cross-provider tool defs into Anthropic SDK tool
// params, reading `required` from the schema verbatim rather than
// synthesizing it from property names.
func BuildTools(in []llm.ToolDef) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, tool := range in {
		schema := tool.Schema
		var required []string
		if raw, ok := schema["required"].([]any); ok {
			for _, r := range raw {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		} else if raw, ok := schema["required"].([]string); ok {
			required = append(required, raw...)
		}
		out = append(out, anthropic.ToolUnionParamOfTool(
			anthropic.ToolInputSchemaParam{
				Properties: schema,
				Required:   required,
			},
			tool.Name,
		))
	}
	return out
}

type Options struct {
	APIKey  string
	BaseURL string
}

type Client struct {
	apiKey  string
	baseURL string
}

func New(opts Options) (*Client, error) {
	client := &Client{
		apiKey:  opts.APIKey,
		baseURL: strings.TrimSuffix(opts.BaseURL, "/"),
	}
	return client, nil
}

func (c *Client) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	auth := c.apiKey
	if auth == "" {
		return nil, fmt.Errorf("no API key provided")
	}

	baseURL := c.baseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	client := anthropic.NewClient(
		option.WithAPIKey(auth),
		option.WithBaseURL(baseURL),
	)

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 32768
	}

	msgs := BuildMessages(req.Messages)
	tools := BuildTools(req.Tools)

	// Create request params
	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
		Tools:     tools,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.ThinkingBudgetTokens > 0 {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(req.ThinkingBudgetTokens))
	}

	stream := client.Messages.NewStreaming(ctx, params)
	// Check for immediate error (e.g., invalid params before HTTP call)
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("stream error: %w", err)
	}

	ch := make(chan llm.StreamEvent, 64)
	go readStream(stream, ch)
	return ch, nil
}

func readStream(stream *ssestream.Stream[anthropic.MessageStreamEventUnion], out chan<- llm.StreamEvent) {
	defer close(out)

	var currentTool struct {
		id, name string
		json     strings.Builder
		active   bool
	}
	var currentThinking struct {
		active    bool
		signature string
	}

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			out <- llm.StreamEvent{
				Type:               llm.EventMessageStart,
				InputTokens:        int(e.Message.Usage.InputTokens),
				OutputTokens:       int(e.Message.Usage.OutputTokens),
				CacheReadInput:     int(e.Message.Usage.CacheReadInputTokens),
				CacheCreationInput: int(e.Message.Usage.CacheCreationInputTokens),
			}

		case anthropic.ContentBlockStartEvent:
			cb := e.ContentBlock
			switch cb.Type {
			case "tool_use":
				currentTool.id = cb.ID
				currentTool.name = cb.Name
				currentTool.json.Reset()
				currentTool.active = true
				out <- llm.StreamEvent{
					Type:      llm.EventToolUseStart,
					ToolUseID: currentTool.id,
					ToolName:  currentTool.name,
				}
			case "thinking":
				currentThinking.active = true
				currentThinking.signature = cb.Signature
			case "text":
				out <- llm.StreamEvent{Type: llm.EventContentBlockStart}
			}

		case anthropic.ContentBlockDeltaEvent:
			switch e.Delta.Type {
			case "text_delta":
				out <- llm.StreamEvent{
					Type: llm.EventTextDelta,
					Text: e.Delta.Text,
				}
			case "thinking_delta":
				out <- llm.StreamEvent{
					Type: llm.EventThinkingDelta,
					Text: e.Delta.Thinking,
				}
			case "signature_delta":
				if currentThinking.active && e.Delta.Signature != "" {
					currentThinking.signature = e.Delta.Signature
				}
			case "input_json_delta":
				if currentTool.active {
					out <- llm.StreamEvent{
						Type:        llm.EventToolUseDelta,
						ToolUseID:   currentTool.id,
						PartialJSON: e.Delta.PartialJSON,
					}
				}
			}

		case anthropic.ContentBlockStopEvent:
			if currentTool.active {
				out <- llm.StreamEvent{Type: llm.EventToolUseStop}
				currentTool.active = false
			}
			if currentThinking.active {
				out <- llm.StreamEvent{
					Type:      llm.EventThinkingStop,
					Signature: currentThinking.signature,
				}
				currentThinking.active = false
				currentThinking.signature = ""
			}

		case anthropic.MessageDeltaEvent:
			out <- llm.StreamEvent{
				Type:               llm.EventMessageStop,
				StopReason:         string(e.Delta.StopReason),
				InputTokens:        int(e.Usage.InputTokens),
				OutputTokens:       int(e.Usage.OutputTokens),
				CacheReadInput:     int(e.Usage.CacheReadInputTokens),
				CacheCreationInput: int(e.Usage.CacheCreationInputTokens),
			}

		case anthropic.MessageStopEvent:
			out <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: ""}
		}
	}

	if err := stream.Err(); err != nil {
		out <- llm.StreamEvent{Type: llm.EventError, Err: err}
	}
}
