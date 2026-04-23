package openaicompat

// chatRequest is the outbound body for POST /chat/completions.
type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []chatMessage  `json:"messages"`
	Stream              bool           `json:"stream"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
	Tools               []chatTool     `json:"tools,omitempty"`
	ToolChoice          any            `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool          `json:"parallel_tool_calls,omitempty"`
	MaxTokens           *int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	PresencePenalty     *float64       `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64       `json:"frequency_penalty,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content,omitempty"`
	Reasoning  string         `json:"reasoning,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// streamChunk is one SSE frame's JSON body.
type streamChunk struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	SystemFingerprint string         `json:"system_fingerprint"`
	Choices           []streamChoice `json:"choices"`
	Usage             *streamUsage   `json:"usage,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	FinishReason *string     `json:"finish_reason"`
	Delta        streamDelta `json:"delta"`
}

type streamDelta struct {
	Role             string                `json:"role,omitempty"`
	Content          *string               `json:"content,omitempty"`
	Refusal          *string               `json:"refusal,omitempty"`
	ReasoningContent string                `json:"reasoning_content,omitempty"`
	Reasoning        string                `json:"reasoning,omitempty"`
	ToolCalls        []streamToolCallDelta `json:"tool_calls,omitempty"`
}

type streamToolCallDelta struct {
	Index    int                     `json:"index"`
	ID       string                  `json:"id,omitempty"`
	Type     string                  `json:"type,omitempty"`
	Function streamToolCallDeltaFunc `json:"function"`
}

type streamToolCallDeltaFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type streamUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	CompletionTokensDetails streamUsageCompletionDet `json:"completion_tokens_details"`
}

type streamUsageCompletionDet struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}
