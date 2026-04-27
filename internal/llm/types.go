package llm

import "encoding/json"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ContentType string

const (
	ContentText       ContentType = "text"
	ContentThinking   ContentType = "thinking"
	ContentToolUse    ContentType = "tool_use"
	ContentToolResult ContentType = "tool_result"
	ContentImage      ContentType = "image"
)

type ContentBlock struct {
	Type      ContentType     `json:"type"`
	Text      string          `json:"text,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	ImageData string          `json:"image_data,omitempty"` // base64 image payload (no data: prefix)
	MediaType string          `json:"media_type,omitempty"` // e.g. "image/png", "image/jpeg"
}

// ImageAttachment carries a single processed image to be attached to a user
// turn. Data is the raw bytes (not base64 encoded); the agent base64-encodes
// when constructing ContentImage blocks.
type ImageAttachment struct {
	Data      []byte
	MediaType string
	Width     int
	Height    int
}

type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

type Request struct {
	Model                string
	System               string
	Messages             []Message
	Tools                []ToolDef
	MaxTokens            int
	ThinkingBudgetTokens int
}

type StreamEventType string

const (
	EventMessageStart      StreamEventType = "message_start"
	EventContentBlockStart StreamEventType = "content_block_start"
	EventTextDelta         StreamEventType = "text_delta"
	EventThinkingDelta     StreamEventType = "thinking_delta"
	EventThinkingStop      StreamEventType = "thinking_stop"
	EventToolUseStart      StreamEventType = "tool_use_start"
	EventToolUseDelta      StreamEventType = "tool_use_delta"
	EventToolUseStop       StreamEventType = "tool_use_stop"
	EventMessageStop       StreamEventType = "message_stop"
	EventError             StreamEventType = "error"
)

type StreamEvent struct {
	Type               StreamEventType
	Text               string
	Signature          string
	ToolUseID          string
	ToolName           string
	PartialJSON        string
	StopReason         string
	InputTokens        int
	OutputTokens       int
	CacheReadInput     int
	CacheCreationInput int
	Err                error
}
