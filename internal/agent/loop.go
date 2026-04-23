package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

func (a *Agent) turn(ctx context.Context, s submit) {
	// Add user message to history
	a.history = append(a.history, llm.Message{
		Role:    llm.RoleUser,
		Content: []llm.ContentBlock{{Type: llm.ContentText, Text: s.userMsg}},
	})

	for i := 0; i < a.maxIters; i++ {
		req := llm.Request{
			Model:     a.model,
			System:    a.system,
			Messages:  a.history,
			Tools:     a.tools.Defs(),
			MaxTokens: a.maxTokens,
		}

		asst, pending, stop, err := a.consumeStream(ctx, req, s.out, s.ctx)
		if asst.Role != "" {
			a.history = append(a.history, asst)
		}
		if err != nil {
			emitToChan(s.out, ErrorEvent{Err: err}, s.ctx)
			return
		}

		if len(pending) == 0 || stop == "end_turn" {
			emitToChan(s.out, TurnDone{StopReason: stop}, s.ctx)
			return
		}

		// Run each tool sequentially
		results := make([]llm.ContentBlock, 0, len(pending))
		for _, call := range pending {
			res := a.runToolCall(ctx, s, call)
			results = append(results, llm.ContentBlock{
				Type:      llm.ContentToolResult,
				ToolUseID: call.ID,
				Output:    res.Output,
				IsError:   res.IsError,
			})
			emitToChan(s.out, ToolResult{ID: call.ID, Name: call.Name, Output: res.Output, IsError: res.IsError}, s.ctx)
		}

		a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: results})

		if s.ctx.Err() != nil {
			emitToChan(s.out, ErrorEvent{Err: s.ctx.Err()}, s.ctx)
			return
		}
	}

	emitToChan(s.out, ErrorEvent{Err: fmt.Errorf("agent: max iterations (%d) reached", a.maxIters)}, s.ctx)
}

// consumeStream reads events from the provider and returns accumulated results
func (a *Agent) consumeStream(ctx context.Context, req llm.Request, out chan Event, reqCtx context.Context) (llm.Message, []ToolCall, string, error) {
	var msg llm.Message
	msg.Role = llm.RoleAssistant

	var pending []ToolCall
	var currentTool struct {
		id, name string
		json     strings.Builder
		active   bool
	}

	evs, err := a.provider.Stream(ctx, req)
	if err != nil {
		return msg, nil, "", err
	}

	for ev := range evs {
		switch ev.Type {
		case llm.EventMessageStart:
			emitToChan(out, MessageStart{}, reqCtx)

		case llm.EventThinkingDelta:
			emitToChan(out, ThinkingDelta{Text: ev.Text}, reqCtx)
			if n := len(msg.Content); n > 0 && msg.Content[n-1].Type == llm.ContentThinking {
				msg.Content[n-1].Text += ev.Text
			} else {
				msg.Content = append(msg.Content, llm.ContentBlock{
					Type: llm.ContentThinking,
					Text: ev.Text,
				})
			}

		case llm.EventTextDelta:
			emitToChan(out, TextDelta{Text: ev.Text}, reqCtx)
			if n := len(msg.Content); n > 0 && msg.Content[n-1].Type == llm.ContentText {
				msg.Content[n-1].Text += ev.Text
			} else {
				msg.Content = append(msg.Content, llm.ContentBlock{
					Type: llm.ContentText,
					Text: ev.Text,
				})
			}

		case llm.EventToolUseStart:
			currentTool.id = ev.ToolUseID
			currentTool.name = ev.ToolName
			currentTool.json.Reset()
			currentTool.active = true

		case llm.EventContentBlockStart:
			// Text block start - ignore

		case llm.EventToolUseDelta:
			if currentTool.active {
				currentTool.json.WriteString(ev.PartialJSON)
			}

		case llm.EventToolUseStop:
			if currentTool.active && currentTool.id != "" {
				input := json.RawMessage(currentTool.json.String())
				pending = append(pending, ToolCall{
					ID:    currentTool.id,
					Name:  currentTool.name,
					Input: input,
				})
				msg.Content = append(msg.Content, llm.ContentBlock{
					Type:      llm.ContentToolUse,
					ToolUseID: currentTool.id,
					ToolName:  currentTool.name,
					Input:     input,
				})
				emitToChan(out, pending[len(pending)-1], reqCtx)
			}
			currentTool.active = false
			currentTool.id = ""
			currentTool.name = ""

		case llm.EventMessageStop:
			if ev.InputTokens > 0 || ev.OutputTokens > 0 || ev.CacheReadInput > 0 || ev.CacheCreationInput > 0 {
				emitToChan(out, UsageEvent{
					InputTokens:        ev.InputTokens,
					OutputTokens:       ev.OutputTokens,
					CacheReadInput:     ev.CacheReadInput,
					CacheCreationInput: ev.CacheCreationInput,
				}, reqCtx)
			}
			emitToChan(out, MessageEnd{StopReason: ev.StopReason}, reqCtx)
			return msg, pending, ev.StopReason, nil

		case llm.EventError:
			return msg, pending, "", ev.Err
		}
	}

	// Channel closed without message_stop
	if reqCtx.Err() != nil {
		return msg, pending, "", reqCtx.Err()
	}
	return msg, pending, "", fmt.Errorf("stream closed prematurely")
}

// runToolCall executes a single tool call
func (a *Agent) runToolCall(ctx context.Context, s submit, call ToolCall) tools.Result {
	// Look up tool
	t, ok := a.tools.Get(call.Name)
	if !ok {
		return tools.Result{
			Output:  fmt.Sprintf("unknown tool: %s", call.Name),
			IsError: true,
		}
	}

	// Check policy
	decision := a.policy.Check(call.Name, call.Input)

	switch decision {
	case policy.Deny:
		return tools.Result{
			Output:  "denied by user",
			IsError: true,
		}

	case policy.Ask:
		// Block and wait for approval
		reply := make(chan ApprovalDecision, 1)
		req := ApprovalRequest{
			ID:    call.ID,
			Tool:  call.Name,
			Input: call.Input,
			reply: reply,
		}
		emitToChan(s.out, req, s.ctx)

		// Wait for response or cancellation
		select {
		case decision := <-reply:
			if decision.Kind == DecisionDeny {
				return tools.Result{
					Output:  "denied by user",
					IsError: true,
				}
			}
			if decision.Kind == DecisionAllowSession {
				a.policy.AllowSession(call.Name)
			}
		case <-s.ctx.Done():
			return tools.Result{
				Output:  "cancelled",
				IsError: true,
			}
		}
	}

	// Execute tool
	result, err := t.Run(ctx, call.Input)
	if err != nil {
		return tools.Result{
			Output:  fmt.Sprintf("tool error: %v", err),
			IsError: true,
		}
	}

	// Track read files
	if call.Name == "Read" && !result.IsError {
		var input struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(call.Input, &input); err == nil && input.FilePath != "" {
			a.readFiles[input.FilePath] = struct{}{}
		}
	}

	return result
}

func emitToChan(out chan Event, e Event, ctx context.Context) {
	select {
	case out <- e:
	case <-ctx.Done():
	}
}
