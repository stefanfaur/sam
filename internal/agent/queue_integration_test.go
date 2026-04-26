package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

type noopInput struct{}

// noopTool succeeds with a fixed output. Lets us drive the agent through a
// real tool dispatch boundary without filesystem side effects.
func noopTool() tools.Tool {
	return tools.New[noopInput]("Noop", "no-op test tool",
		func(ctx context.Context, _ noopInput) (tools.Result, error) {
			return tools.Result{Output: "ok"}, nil
		})
}

// TestQueueDrainsAtIterationBoundary runs a turn where iter 1 yields a tool
// call and iter 2 ends the turn. The user queues a steer between iters; the
// drain should append the queue text to the tool_results message that lands
// in iter 2's request.
func TestQueueDrainsAtIterationBoundary(t *testing.T) {
	prov := fake.New()

	a := New(Options{
		Provider: prov,
		Tools:    func() *tools.Registry { r := tools.NewRegistry(); r.Register(noopTool()); return r }(),
		Policy:   func() *policy.Policy { p := policy.AllowAll(); p.AllowSession("Noop"); return p }(),
		MaxIters: 5,
	})

	prov.StreamFn = func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		switch len(prov.Calls) {
		case 1:
			// Queue mid-iter-1 (after the user message landed in history but
			// before iter 2's request is built) so the boundary drain has
			// something to merge.
			a.QueueSteer("midstream addition")
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "t1", ToolName: "Noop"}
			ch <- llm.StreamEvent{Type: llm.EventToolUseDelta, ToolUseID: "t1", PartialJSON: `{}`}
			ch <- llm.StreamEvent{Type: llm.EventToolUseStop, ToolUseID: "t1"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "tool_use"}
		default:
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: "done"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "end_turn"}
		}
		close(ch)
		return ch, nil
	}

	a.Start()
	defer a.Close()

	events := a.Submit(context.Background(), "kickoff")
	timeout := time.After(3 * time.Second)
	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-timeout:
		t.Fatalf("turn did not complete")
	}

	// After the turn, the steer queue must be empty (drained at boundary or
	// auto-submitted on end_turn).
	if got := len(a.GetQueue()); got != 0 {
		t.Fatalf("expected queue drained, got %d items: %v", got, a.GetQueue())
	}

	// Inspect iter 2's request: the last user message before the assistant
	// reply must hold both the tool_result AND the merged steer text.
	if len(prov.Calls) < 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(prov.Calls))
	}
	iter2 := prov.Calls[1]
	var lastUser *llm.Message
	for i := len(iter2.Messages) - 1; i >= 0; i-- {
		if iter2.Messages[i].Role == llm.RoleUser {
			lastUser = &iter2.Messages[i]
			break
		}
	}
	if lastUser == nil {
		t.Fatalf("iter 2 has no user message")
	}
	var sawToolResult, sawSteer bool
	for _, blk := range lastUser.Content {
		if blk.Type == llm.ContentToolResult {
			sawToolResult = true
		}
		if blk.Type == llm.ContentText && strings.Contains(blk.Text, "midstream addition") {
			sawSteer = true
		}
	}
	if !sawToolResult {
		t.Fatalf("iter 2's last user message missing tool_result block: %+v", lastUser)
	}
	if !sawSteer {
		t.Fatalf("iter 2's last user message missing merged steer text: %+v", lastUser)
	}
}

// TestQueueAutoSubmitsOnEndTurn drives a turn where iter 1 is a clean
// end_turn. With queue non-empty the loop must continue to iter 2 with the
// queue text as a fresh user message.
func TestQueueAutoSubmitsOnEndTurn(t *testing.T) {
	prov := fake.New()
	a := New(Options{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		MaxIters: 5,
	})

	prov.StreamFn = func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 4)
		if len(prov.Calls) == 1 {
			// Queue before iter 1's events drain so the end_turn path picks
			// up the queue and auto-submits it as iter 2's user message.
			a.QueueSteer("follow-up question")
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: "all done"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "end_turn"}
		} else {
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: "thanks"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "end_turn"}
		}
		close(ch)
		return ch, nil
	}

	a.Start()
	defer a.Close()

	events := a.Submit(context.Background(), "first")
	for range events {
	}

	if got := len(a.GetQueue()); got != 0 {
		t.Fatalf("queue should be drained, got %d", got)
	}
	if len(prov.Calls) != 2 {
		t.Fatalf("expected 2 provider calls (auto-submit), got %d", len(prov.Calls))
	}
	iter2 := prov.Calls[1]
	last := iter2.Messages[len(iter2.Messages)-1]
	if last.Role != llm.RoleUser {
		t.Fatalf("expected last message to be user, got role=%s", last.Role)
	}
	if len(last.Content) != 1 || last.Content[0].Type != llm.ContentText ||
		!strings.Contains(last.Content[0].Text, "follow-up question") {
		t.Fatalf("auto-submit user message wrong: %+v", last)
	}
}

// TestQueuePreservedOnProviderError ensures a provider error leaves the queue
// untouched and the next Submit prepends the queue text onto the new user
// message.
func TestQueuePreservedOnProviderError(t *testing.T) {
	prov := fake.New()
	a := New(Options{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		MaxIters: 5,
	})

	prov.StreamFn = func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
		switch len(prov.Calls) {
		case 1:
			// Queue *during* iter 1 (after the initial user message has
			// already been added + prependQueuedText drained). This mirrors
			// the real failure window: user typed mid-turn, then the
			// provider blew up.
			a.QueueSteer("queued during failure window")
			return nil, errors.New("simulated provider failure")
		default:
			ch := make(chan llm.StreamEvent, 4)
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: "ok"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "end_turn"}
			close(ch)
			return ch, nil
		}
	}

	a.Start()
	defer a.Close()

	// Turn 1: fails after queue is set inside StreamFn. Queue must persist.
	events := a.Submit(context.Background(), "first")
	var sawError bool
	for ev := range events {
		if _, ok := ev.(ErrorEvent); ok {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected ErrorEvent on first turn")
	}
	if got := len(a.GetQueue()); got != 1 {
		t.Fatalf("expected queue preserved (1 item), got %d", got)
	}

	// Turn 2: the queued text rides on the new user message.
	events = a.Submit(context.Background(), "second")
	for range events {
	}
	if got := len(a.GetQueue()); got != 0 {
		t.Fatalf("expected queue drained after second turn, got %d", got)
	}
	if len(prov.Calls) < 2 {
		t.Fatalf("expected at least 2 provider calls, got %d", len(prov.Calls))
	}
	iter2 := prov.Calls[1]
	// Find the user message that was just submitted (the last user message in
	// history at the moment iter 2's request was built).
	var newUser *llm.Message
	for i := len(iter2.Messages) - 1; i >= 0; i-- {
		if iter2.Messages[i].Role == llm.RoleUser {
			newUser = &iter2.Messages[i]
			break
		}
	}
	if newUser == nil {
		t.Fatalf("no user message in iter 2")
	}
	var sawPrepend, sawOriginal bool
	for _, blk := range newUser.Content {
		if blk.Type != llm.ContentText {
			continue
		}
		if strings.Contains(blk.Text, "queued during failure window") {
			sawPrepend = true
		}
		if strings.Contains(blk.Text, "second") {
			sawOriginal = true
		}
	}
	if !sawPrepend {
		t.Fatalf("queue text not prepended onto next user message: %+v", newUser)
	}
	if !sawOriginal {
		t.Fatalf("original user text missing: %+v", newUser)
	}
}

// TestRoleAlternationMaintained verifies the merge path produces the
// alternation pattern strict providers (DeepSeek) require: user → assistant →
// user(tool_result + merged_steer) → assistant — not user → user.
func TestRoleAlternationMaintained(t *testing.T) {
	prov := fake.New()
	a := New(Options{
		Provider: prov,
		Tools:    func() *tools.Registry { r := tools.NewRegistry(); r.Register(noopTool()); return r }(),
		Policy:   func() *policy.Policy { p := policy.AllowAll(); p.AllowSession("Noop"); return p }(),
		MaxIters: 5,
	})

	prov.StreamFn = func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		switch len(prov.Calls) {
		case 1:
			a.QueueSteer("steer text")
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "t1", ToolName: "Noop"}
			ch <- llm.StreamEvent{Type: llm.EventToolUseDelta, ToolUseID: "t1", PartialJSON: `{}`}
			ch <- llm.StreamEvent{Type: llm.EventToolUseStop, ToolUseID: "t1"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "tool_use"}
		default:
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: "ok"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "end_turn"}
		}
		close(ch)
		return ch, nil
	}

	a.Start()
	defer a.Close()

	events := a.Submit(context.Background(), "go")
	for range events {
	}

	// History must alternate strictly.
	prev := llm.Role("")
	for i, m := range a.history {
		if m.Role == prev {
			t.Fatalf("role alternation violated at index %d: %s → %s\nhistory=%+v",
				i, prev, m.Role, a.history)
		}
		prev = m.Role
	}
}
