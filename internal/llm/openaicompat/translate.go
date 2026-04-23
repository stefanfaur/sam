package openaicompat

import (
	"encoding/json"
	"strings"

	"github.com/stefanfaur/sam/internal/llm"
)

// strPtr returns a pointer to the given string. Used so chatMessage.Content's
// `*string` with `omitempty` distinguishes "empty string" (ptr to "") from
// "absent field" (nil ptr).
func strPtr(s string) *string { return &s }

// toWireMessages projects the cross-provider history (sys + []llm.Message)
// into the OpenAI-compat chat message sequence per spec §3.
func toWireMessages(sys string, msgs []llm.Message, caps Capabilities) []chatMessage {
	var out []chatMessage
	if sys != "" {
		out = append(out, chatMessage{Role: caps.SystemRole, Content: strPtr(sys)})
	}
	for _, msg := range msgs {
		switch msg.Role {
		case llm.RoleUser:
			out = append(out, translateUserMessage(msg)...)
		case llm.RoleAssistant:
			if m, ok := translateAssistantMessage(msg, caps); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func translateUserMessage(msg llm.Message) []chatMessage {
	var out []chatMessage
	var text strings.Builder
	for _, b := range msg.Content {
		if b.Type == llm.ContentText {
			text.WriteString(b.Text)
		}
	}
	if text.Len() > 0 {
		out = append(out, chatMessage{Role: "user", Content: strPtr(text.String())})
	}
	for _, b := range msg.Content {
		if b.Type != llm.ContentToolResult {
			continue
		}
		content := b.Output
		if b.IsError {
			payload := struct {
				Error  bool   `json:"error"`
				Output string `json:"output"`
			}{Error: true, Output: b.Output}
			if buf, err := json.Marshal(payload); err == nil {
				content = string(buf)
			}
		}
		out = append(out, chatMessage{
			Role:       "tool",
			ToolCallID: b.ToolUseID,
			Content:    strPtr(content),
		})
	}
	return out
}

func translateAssistantMessage(msg llm.Message, caps Capabilities) (chatMessage, bool) {
	var text strings.Builder
	var thinking strings.Builder
	var toolCalls []chatToolCall
	for _, b := range msg.Content {
		switch b.Type {
		case llm.ContentText:
			text.WriteString(b.Text)
		case llm.ContentThinking:
			if caps.EchoReasoning {
				thinking.WriteString(b.Text)
			}
		case llm.ContentToolUse:
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, chatToolCall{
				ID:   b.ToolUseID,
				Type: "function",
				Function: chatToolCallFunc{
					Name:      b.ToolName,
					Arguments: args,
				},
			})
		}
	}
	out := chatMessage{Role: "assistant"}
	if text.Len() > 0 {
		out.Content = strPtr(text.String())
	}
	if thinking.Len() > 0 {
		out.Reasoning = thinking.String()
	}
	if len(toolCalls) > 0 {
		out.ToolCalls = toolCalls
	}
	if out.Content == nil && out.Reasoning == "" && len(out.ToolCalls) == 0 {
		return chatMessage{}, false
	}
	return out, true
}

// toWireTools translates tool defs into the OpenAI function-tool shape.
// Preserves the schema verbatim — including the `required` array if the
// caller supplied one, absent if not.
func toWireTools(tools []llm.ToolDef) []chatTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]chatTool, 0, len(tools))
	for _, td := range tools {
		out = append(out, chatTool{
			Type: "function",
			Function: chatToolFunc{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.Schema,
			},
		})
	}
	return out
}

// buildRequest assembles the full outbound chatRequest from the cross-provider
// llm.Request, the resolved Capabilities, and the per-model reasoning effort.
func buildRequest(req llm.Request, caps Capabilities, effort string) chatRequest {
	r := chatRequest{
		Model:    req.Model,
		Messages: toWireMessages(req.System, req.Messages, caps),
		Stream:   true,
	}
	if caps.SupportsIncludeUsage {
		r.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if tools := toWireTools(req.Tools); len(tools) > 0 {
		r.Tools = tools
	}
	if req.MaxTokens > 0 {
		n := req.MaxTokens
		switch caps.MaxTokensField {
		case "max_completion_tokens":
			r.MaxCompletionTokens = &n
		default:
			r.MaxTokens = &n
		}
	}
	// Sampling params left nil when unsupported (no default temperature).
	_ = caps.SupportsSamplingParams
	if caps.SupportsReasoningEffort && effort != "" {
		r.ReasoningEffort = effort
	}
	// parallel_tool_calls: emit only when forcing false.
	if !caps.SupportsParallelToolCalls {
		f := false
		r.ParallelToolCalls = &f
	}
	return r
}
