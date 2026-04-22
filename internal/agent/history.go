package agent

import (
	"fmt"

	"github.com/stefanfaur/sam/internal/llm"
)

// wellFormed verifies every tool_use in assistant messages has a matching
// tool_result in a subsequent user message. Dev-only sanity helper.
func wellFormed(msgs []llm.Message) error {
	pending := map[string]struct{}{}
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleAssistant:
			for _, b := range m.Content {
				if b.Type == llm.ContentToolUse {
					pending[b.ToolUseID] = struct{}{}
				}
			}
		case llm.RoleUser:
			for _, b := range m.Content {
				if b.Type == llm.ContentToolResult {
					delete(pending, b.ToolUseID)
				}
			}
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("unmatched tool_use ids: %v", keys(pending))
	}
	return nil
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
