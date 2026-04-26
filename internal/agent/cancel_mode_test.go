package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// TestCancelTurn_GranularDuringStream confirms a granular cancel mid-stream
// closes the turn cleanly with TurnDone "cancelled" and leaves history
// wire-valid (no orphan tool_use blocks).
func TestCancelTurn_GranularDuringStream(t *testing.T) {
	prov := &fake.Provider{}
	started := make(chan struct{})
	prov.StreamFn = func(ctx context.Context, _ llm.Request) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 4)
		go func() {
			defer close(ch)
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventTextDelta, Text: "thinking..."}
			close(started)
			<-ctx.Done()
		}()
		return ch, nil
	}

	a := New(Options{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		MaxIters: 3,
	})
	a.Start()
	defer a.Close()

	events := a.Submit(context.Background(), "hi")

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider stream never started")
	}

	a.CancelTurn(CancelModeGranular)

	var doneReason string
	for ev := range events {
		if td, ok := ev.(TurnDone); ok {
			doneReason = td.StopReason
		}
	}
	if doneReason != "cancelled" {
		t.Fatalf("expected TurnDone 'cancelled', got %q", doneReason)
	}
	for _, msg := range a.history {
		for _, b := range msg.Content {
			if b.Type == llm.ContentToolUse {
				t.Fatalf("history contains orphan tool_use block: %+v", b)
			}
		}
	}
}

// TestCancelTurn_AbortAfterToolUseSkipsDispatch checks that an abort cancel
// after the model emitted tool_use blocks (but before dispatch) injects
// synthetic cancelled results and exits without running tools.
func TestCancelTurn_AbortAfterToolUseSkipsDispatch(t *testing.T) {
	prov := &fake.Provider{}
	emitted := make(chan struct{})
	prov.StreamFn = func(ctx context.Context, _ llm.Request) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		go func() {
			defer close(ch)
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			ch <- llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "t1", ToolName: "Bash"}
			ch <- llm.StreamEvent{Type: llm.EventToolUseDelta, ToolUseID: "t1", PartialJSON: `{"command":"sleep 5"}`}
			ch <- llm.StreamEvent{Type: llm.EventToolUseStop, ToolUseID: "t1"}
			ch <- llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "tool_use"}
			close(emitted)
		}()
		return ch, nil
	}

	// Slow tool: blocks until ctx cancellation. If the agent dispatches it,
	// we'll observe a delay; abort path should skip dispatch entirely.
	type bashIn struct {
		Command string `json:"command"`
	}
	reg := tools.NewRegistry()
	dispatched := make(chan struct{}, 1)
	reg.Register(tools.New[bashIn]("Bash", "test bash",
		func(ctx context.Context, _ bashIn) (tools.Result, error) {
			select {
			case dispatched <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return tools.Result{Output: "ran", IsError: false}, nil
		}))

	a := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
		MaxIters: 3,
	})
	a.Start()
	defer a.Close()

	events := a.Submit(context.Background(), "do work")

	// Wait for the tool_use to be fully streamed, then abort BEFORE the loop
	// starts dispatching.
	select {
	case <-emitted:
	case <-time.After(2 * time.Second):
		t.Fatal("provider stream never finished emitting")
	}
	a.CancelTurn(CancelModeAbort)

	var (
		toolResults []ToolResult
		doneReason  string
	)
	for ev := range events {
		switch e := ev.(type) {
		case ToolResult:
			toolResults = append(toolResults, e)
		case TurnDone:
			doneReason = e.StopReason
		}
	}

	if doneReason != "cancelled" {
		t.Fatalf("expected TurnDone 'cancelled', got %q", doneReason)
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 synthetic ToolResult, got %d", len(toolResults))
	}
	tr := toolResults[0]
	if !tr.IsError || tr.Output != "cancelled" {
		t.Fatalf("expected cancelled error result, got %+v", tr)
	}
}

// TestCancelCurrent_BackCompat confirms the deprecated CancelCurrent wrapper
// still routes to granular semantics.
func TestCancelCurrent_BackCompat(t *testing.T) {
	prov := &fake.Provider{}
	started := make(chan struct{})
	prov.StreamFn = func(ctx context.Context, _ llm.Request) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 1)
		go func() {
			defer close(ch)
			ch <- llm.StreamEvent{Type: llm.EventMessageStart}
			close(started)
			<-ctx.Done()
		}()
		return ch, nil
	}

	a := New(Options{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		MaxIters: 1,
	})
	a.Start()
	defer a.Close()

	events := a.Submit(context.Background(), "hi")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream never started")
	}
	a.CancelCurrent()

	for range events {
	}
	if a.cancelMode != CancelModeGranular {
		t.Fatalf("expected granular mode after CancelCurrent, got %v", a.cancelMode)
	}
}
