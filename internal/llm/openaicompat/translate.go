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
// into the OpenAI-compat chat message sequence.
//
// Three caps-driven behaviors affect the system text:
//   - PrependFormatting: literal "Formatting re-enabled.\n" prepended (o-series).
//   - SystemRole=="user": wrap sys in <system>...</system> and splice into the
//     first user message instead of emitting a separate system message
//     (self-hosted original R1 weights).
//   - Otherwise: emit as a standalone first message with role=caps.SystemRole.
func toWireMessages(sys string, msgs []llm.Message, caps Capabilities) []chatMessage {
	if sys != "" && caps.PrependFormatting {
		sys = "Formatting re-enabled.\n" + sys
	}

	var out []chatMessage
	emitSystem := sys != "" && caps.SystemRole != "user"
	if emitSystem {
		out = append(out, chatMessage{Role: caps.SystemRole, Content: strPtr(sys)})
	}

	for _, msg := range msgs {
		switch msg.Role {
		case llm.RoleUser:
			translated := translateUserMessage(msg)
			if sys != "" && caps.SystemRole == "user" {
				spliced := false
				for i := range translated {
					if translated[i].Role == "user" && translated[i].Content != nil {
						wrapped := "<system>\n" + sys + "\n</system>\n\n" + *translated[i].Content
						translated[i].Content = strPtr(wrapped)
						spliced = true
						break
					}
				}
				if !spliced {
					wrapped := "<system>\n" + sys + "\n</system>"
					translated = append([]chatMessage{{Role: "user", Content: strPtr(wrapped)}}, translated...)
				}
				sys = ""
			}
			out = append(out, translated...)
		case llm.RoleAssistant:
			if m, ok := translateAssistantMessage(msg, caps); ok {
				out = append(out, m)
			}
		}
	}

	if sys != "" && caps.SystemRole == "user" {
		wrapped := "<system>\n" + sys + "\n</system>"
		out = append(out, chatMessage{Role: "user", Content: strPtr(wrapped)})
	}

	return out
}

func translateUserMessage(msg llm.Message) []chatMessage {
	var out []chatMessage
	var text strings.Builder
	hasImage := false
	for _, b := range msg.Content {
		switch b.Type {
		case llm.ContentText:
			text.WriteString(b.Text)
		case llm.ContentImage:
			if b.ImageData != "" && b.MediaType != "" {
				hasImage = true
			}
		}
	}
	if hasImage {
		parts := make([]contentPart, 0, len(msg.Content)+1)
		if text.Len() > 0 {
			parts = append(parts, contentPart{Type: "text", Text: text.String()})
		}
		for _, b := range msg.Content {
			if b.Type != llm.ContentImage || b.ImageData == "" || b.MediaType == "" {
				continue
			}
			parts = append(parts, contentPart{
				Type: "image_url",
				ImageURL: &contentPartImage{
					URL: "data:" + b.MediaType + ";base64," + b.ImageData,
				},
			})
		}
		out = append(out, chatMessage{Role: "user", MultiContent: parts})
	} else if text.Len() > 0 {
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
		// Some servers (Moonshot, DeepSeek) validate the field name matches
		// the stream's ReasoningSource; others accept either. Route by caps.
		switch caps.ReasoningSource {
		case "reasoning_content", "both":
			out.ReasoningContent = thinking.String()
		default:
			out.Reasoning = thinking.String()
		}
	}
	if len(toolCalls) > 0 {
		out.ToolCalls = toolCalls
		// Trinity (and some other OpenAI-compat servers) reject assistant
		// messages with tool_calls and a null content field. Force content
		// to "" so JSON marshals `"content":""`. Safe on all tested servers.
		if out.Content == nil {
			out.Content = strPtr("")
		}
	}
	if out.Content == nil && out.Reasoning == "" && out.ReasoningContent == "" && len(out.ToolCalls) == 0 {
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
