package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// parallelToolSem caps concurrent parallel-safe tool executions across the
// process. A turn rarely emits more than a handful of parallel-safe calls, so
// 8 covers typical fan-out without a config knob.
var parallelToolSem = make(chan struct{}, 8)

func (a *Agent) turn(ctx context.Context, s submit) {
	// Add user message to history
	a.history = append(a.history, llm.Message{
		Role:    llm.RoleUser,
		Content: []llm.ContentBlock{{Type: llm.ContentText, Text: s.userMsg}},
	})

	for i := 0; i < a.maxIters; i++ {
		a.mu.Lock()
		model := a.model
		resolver := a.maxTokensResolver
		maxTokens := a.maxTokens
		a.mu.Unlock()
		if resolver != nil {
			if v := resolver(model); v > 0 {
				maxTokens = v
			}
		}
		req := llm.Request{
			Model:     model,
			System:    a.system,
			Messages:  a.history,
			Tools:     a.tools.Defs(),
			MaxTokens: maxTokens,
		}

		asst, pending, stop, err := a.consumeStream(ctx, req, s.out, s.ctx)
		// Only remember assistant turns that actually produced content.
		// Cancelled-before-any-block turns leave asst.Role set but
		// asst.Content empty — appending them breaks the next wire call
		// on providers that reject empty-content messages (DeepSeek).
		// On error before tool dispatch, also strip tool_use blocks so
		// we don't leave orphan tool_use ids without matching
		// tool_result follow-ups (also rejected by DeepSeek).
		if err != nil && len(asst.Content) > 0 {
			filtered := asst.Content[:0]
			for _, b := range asst.Content {
				if b.Type != llm.ContentToolUse {
					filtered = append(filtered, b)
				}
			}
			asst.Content = filtered
		}
		if asst.Role != "" && len(asst.Content) > 0 {
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

		results := make([]llm.ContentBlock, len(pending))
		i := 0
		for i < len(pending) {
			headTool, _ := a.tools.Get(pending[i].Name)
			if headTool == nil || !headTool.ParallelSafe() {
				results[i] = a.execOne(ctx, s, pending[i])
				i++
				continue
			}
			j := i + 1
			for j < len(pending) {
				t, _ := a.tools.Get(pending[j].Name)
				if t == nil || !t.ParallelSafe() {
					break
				}
				j++
			}
			a.execParallelGroup(ctx, s, pending[i:j], results[i:j])
			i = j
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

		case llm.EventThinkingStop:
			if n := len(msg.Content); n > 0 && msg.Content[n-1].Type == llm.ContentThinking {
				msg.Content[n-1].Signature = ev.Signature
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

// execParallelGroup runs the given calls concurrently, writing each call's
// resulting ContentBlock into out[idx] at the matching index. Every goroutine
// writes exactly one block, including when the tool panics. Blocks until the
// whole group is joined.
func (a *Agent) execParallelGroup(
	ctx context.Context, s submit,
	calls []ToolCall, out []llm.ContentBlock,
) {
	var wg sync.WaitGroup
	for idx, call := range calls {
		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					out[idx] = llm.ContentBlock{
						Type:      llm.ContentToolResult,
						ToolUseID: call.ID,
						Output:    fmt.Sprintf("panic during tool dispatch: %v", r),
						IsError:   true,
					}
				}
			}()
			parallelToolSem <- struct{}{}
			defer func() { <-parallelToolSem }()
			out[idx] = a.execOne(ctx, s, call)
		}(idx, call)
	}
	wg.Wait()
}

// execOne runs a single tool call, emits its ToolResult event, and returns the
// content block to append to history. Used by both serial and parallel dispatch.
func (a *Agent) execOne(ctx context.Context, s submit, call ToolCall) llm.ContentBlock {
	res := a.runToolCall(ctx, s, call)
	emitToChan(s.out, ToolResult{
		ID:        call.ID,
		Name:      call.Name,
		Output:    res.Output,
		IsError:   res.IsError,
		Rewritten: res.Rewritten,
	}, s.ctx)
	return llm.ContentBlock{
		Type:      llm.ContentToolResult,
		ToolUseID: call.ID,
		Output:    res.Output,
		IsError:   res.IsError,
	}
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

	return result
}

func emitToChan(out chan Event, e Event, ctx context.Context) {
	select {
	case out <- e:
	case <-ctx.Done():
	}
}
