package openaicompat

import (
	"encoding/json"
	"fmt"

	"github.com/stefanfaur/sam/internal/llm"
)

// toolCallState accumulates the per-index state for a streamed tool call:
// the ID seen on the first chunk and the arguments string built up across
// chunks. The registration order is preserved via orderedIdx so we can emit
// EventToolUseStop events deterministically at finish.
type toolCallState struct {
	id, name string
	args     []byte
}

type toolAccumulator struct {
	byIdx      map[int]*toolCallState
	orderedIdx []int
}

func newToolAccumulator() *toolAccumulator {
	return &toolAccumulator{byIdx: map[int]*toolCallState{}}
}

func (a *toolAccumulator) feed(d streamToolCallDelta, out chan<- llm.StreamEvent) {
	st, ok := a.byIdx[d.Index]
	if !ok {
		st = &toolCallState{}
		a.byIdx[d.Index] = st
		a.orderedIdx = append(a.orderedIdx, d.Index)
	}
	if d.ID != "" && st.id == "" {
		st.id = d.ID
	}
	if d.Function.Name != "" && st.name == "" {
		st.name = d.Function.Name
	}
	// Emit EventToolUseStart lazily once we have both id and name. We use
	// args==nil as the "not-yet-started" sentinel; st.args flips to a
	// (possibly empty) byte slice on start.
	if st.args == nil && st.id != "" && st.name != "" {
		out <- llm.StreamEvent{
			Type:      llm.EventToolUseStart,
			ToolUseID: st.id,
			ToolName:  st.name,
		}
		st.args = []byte{}
	}
	if d.Function.Arguments != "" {
		// If start hasn't fired yet (identity fields missing in this chunk),
		// buffer the args. OpenAI normally sends id+name with the first
		// args chunk, so this path is rare; we still handle it.
		if st.args == nil {
			st.args = []byte{}
		}
		st.args = append(st.args, d.Function.Arguments...)
		out <- llm.StreamEvent{
			Type:        llm.EventToolUseDelta,
			ToolUseID:   st.id,
			PartialJSON: d.Function.Arguments,
		}
	}
}

// mapFinishReason converts OpenAI finish_reason strings into the cross-provider
// stop-reason vocabulary consumed by the agent loop.
func mapFinishReason(reason string) string {
	switch reason {
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "content_filter"
	case "", "null":
		return "end_turn"
	default:
		return reason
	}
}

// translate consumes decoded streamChunks and emits llm.StreamEvents.
// It owns the tool-call accumulator and (optionally) the <think>-tag parser
// used when the entry has ParseThinkTags enabled.
func translate(chunks <-chan streamChunk, caps Capabilities, tagParser *Parser) <-chan llm.StreamEvent {
	out := make(chan llm.StreamEvent, 64)
	go func() {
		defer close(out)
		tools := newToolAccumulator()
		var firstChunk = true
		var lastUsage *streamUsage
		var finishReason string
		var sawFinish bool
		var refused bool

		for c := range chunks {
			if firstChunk {
				out <- llm.StreamEvent{Type: llm.EventMessageStart}
				firstChunk = false
			}
			if c.Usage != nil {
				lastUsage = c.Usage
			}
			for _, choice := range c.Choices {
				d := choice.Delta
				// Order: content → reasoning → tool_calls → refusal → finish_reason
				if d.Content != nil && *d.Content != "" {
					if tagParser != nil {
						text, think := tagParser.Feed(*d.Content)
						if text != "" {
							out <- llm.StreamEvent{Type: llm.EventTextDelta, Text: text}
						}
						if think != "" {
							out <- llm.StreamEvent{Type: llm.EventThinkingDelta, Text: think}
						}
					} else {
						out <- llm.StreamEvent{Type: llm.EventTextDelta, Text: *d.Content}
					}
				}
				if d.ReasoningContent != "" {
					out <- llm.StreamEvent{Type: llm.EventThinkingDelta, Text: d.ReasoningContent}
				}
				if d.Reasoning != "" {
					out <- llm.StreamEvent{Type: llm.EventThinkingDelta, Text: d.Reasoning}
				}
				for _, tc := range d.ToolCalls {
					tools.feed(tc, out)
				}
				if d.Refusal != nil && *d.Refusal != "" {
					out <- llm.StreamEvent{Type: llm.EventError, Err: fmt.Errorf("refusal: %s", *d.Refusal)}
					refused = true
				}
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					finishReason = *choice.FinishReason
					sawFinish = true
				}
			}
		}

		// Flush the tag parser's tail, if any.
		if tagParser != nil {
			text, think := tagParser.Flush()
			if text != "" {
				out <- llm.StreamEvent{Type: llm.EventTextDelta, Text: text}
			}
			if think != "" {
				out <- llm.StreamEvent{Type: llm.EventThinkingDelta, Text: think}
			}
		}

		if refused {
			return
		}

		// Finalize tool calls in registration order, validating JSON.
		for _, idx := range tools.orderedIdx {
			st := tools.byIdx[idx]
			if st.id == "" {
				continue
			}
			args := st.args
			if len(args) == 0 {
				args = []byte("{}")
			}
			if !json.Valid(args) {
				out <- llm.StreamEvent{
					Type: llm.EventError,
					Err: fmt.Errorf("tool %q: invalid JSON arguments (finish_reason=%s): %q",
						st.name, finishReason, string(args)),
				}
				return
			}
			out <- llm.StreamEvent{
				Type:      llm.EventToolUseStop,
				ToolUseID: st.id,
			}
		}

		stop := llm.StreamEvent{Type: llm.EventMessageStop, StopReason: mapFinishReason(finishReason)}
		if !sawFinish && len(tools.orderedIdx) == 0 {
			stop.StopReason = "end_turn"
		}
		if lastUsage != nil {
			stop.InputTokens = lastUsage.PromptTokens
			stop.OutputTokens = lastUsage.CompletionTokens
			stop.CacheReadInput = lastUsage.PromptTokensDetails.CachedTokens
		}
		out <- stop
	}()
	return out
}
