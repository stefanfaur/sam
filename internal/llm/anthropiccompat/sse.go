package anthropiccompat

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/stefanfaur/sam/internal/llm"
)

func readSSEStream(r io.Reader, ch chan<- llm.StreamEvent) {
	defer close(ch)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var partialToolID, partialToolName string
	var partialJSON strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}

		eventType, _ := raw["type"].(string)


		switch eventType {
		case "message_start":
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}

		case "content_block_start":
			cb, _ := raw["content_block"].(map[string]interface{})
			if cb != nil {
				cbType, _ := cb["type"].(string)
				if cbType == "tool_use" {
					partialToolID, _ = cb["id"].(string)
					partialToolName, _ = cb["name"].(string)
					partialJSON.Reset()
					ch <- llm.StreamEvent{
						Type:      llm.EventToolUseStart,
						ToolUseID: partialToolID,
						ToolName:  partialToolName,
					}
				} else {
					ch <- llm.StreamEvent{Type: llm.EventContentBlockStart}
				}
			}

		case "content_block_delta":
			delta, _ := raw["delta"].(map[string]interface{})
			if delta != nil {
				deltaType, _ := delta["type"].(string)
				if deltaType == "text_delta" {
					text, _ := delta["text"].(string)
					ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: text}
				} else if deltaType == "input_json_delta" || deltaType == "partial_json" {
					pj, _ := delta["partial_json"].(string)
					partialJSON.WriteString(pj)
					ch <- llm.StreamEvent{
						Type:        llm.EventToolUseDelta,
						ToolUseID:   partialToolID,
						PartialJSON: partialJSON.String(),
					}
				}
			}

		case "content_block_stop":
			ch <- llm.StreamEvent{Type: llm.EventToolUseStop}

		case "message_delta":
			md, _ := raw["delta"].(map[string]interface{})
			if md != nil {
				stopReason, _ := md["stop_reason"].(string)
				ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: stopReason}
			}

		case "ping":
			// Ignore
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- llm.StreamEvent{Type: llm.EventError, Err: err}
	}
}
