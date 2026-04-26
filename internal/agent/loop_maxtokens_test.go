package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

func endTurnScript() fake.Script {
	return fake.Script{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "ok"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	}
}

func runOneTurn(t *testing.T, a *Agent, msg string) {
	t.Helper()
	for range a.Submit(context.Background(), msg) {
	}
}

func TestAgentMaxTokens_UsesResolverWhenPositive(t *testing.T) {
	reg := tools.NewRegistry()
	prov := fake.New(endTurnScript())
	a := New(Options{
		Provider:  prov,
		Tools:     reg,
		Policy:    policy.AllowAll(),
		Model:     "deepseek-v4-pro",
		MaxTokens: 32_768,
		MaxTokensResolverFn: func(m string) int {
			if m == "deepseek-v4-pro" {
				return 192_000
			}
			return 0
		},
	})
	a.Start()
	defer a.Close()
	runOneTurn(t, a, "go")
	if len(prov.Calls) == 0 {
		t.Fatal("no calls")
	}
	if got := prov.Calls[0].MaxTokens; got != 192_000 {
		t.Fatalf("max_tokens: got %d want 192000", got)
	}
}

func TestAgentMaxTokens_FallsBackWhenResolverReturnsZero(t *testing.T) {
	reg := tools.NewRegistry()
	prov := fake.New(endTurnScript())
	a := New(Options{
		Provider:            prov,
		Tools:               reg,
		Policy:              policy.AllowAll(),
		Model:               "claude-sonnet-4-5",
		MaxTokens:           32_768,
		MaxTokensResolverFn: func(string) int { return 0 },
	})
	a.Start()
	defer a.Close()
	runOneTurn(t, a, "go")
	if got := prov.Calls[0].MaxTokens; got != 32_768 {
		t.Fatalf("max_tokens: got %d want 32768", got)
	}
}

func TestAgentMaxTokens_FallsBackWhenResolverNil(t *testing.T) {
	reg := tools.NewRegistry()
	prov := fake.New(endTurnScript())
	a := New(Options{
		Provider:  prov,
		Tools:     reg,
		Policy:    policy.AllowAll(),
		Model:     "claude-sonnet-4-5",
		MaxTokens: 32_768,
	})
	a.Start()
	defer a.Close()
	runOneTurn(t, a, "go")
	if got := prov.Calls[0].MaxTokens; got != 32_768 {
		t.Fatalf("max_tokens: got %d want 32768", got)
	}
}

// Regression: a stream that emits tool_use blocks then closes before
// message_stop used to leave the asst with orphan tool_use ids in
// history (no paired tool_result follow-up). Strict validators
// (DeepSeek) reject the next turn with "tool_use ids were found without
// tool_result blocks immediately after".
func TestAgentStripsOrphanToolUseOnStreamError(t *testing.T) {
	gate := &gateTool{
		name:         "Read",
		parallelSafe: true,
		run: func(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
			return tools.Result{Output: "unreachable"}, nil
		},
	}
	reg := tools.NewRegistry()
	reg.Register(gate)

	// Script emits tool_use blocks then closes WITHOUT message_stop —
	// consumeStream returns an error with the tool_use blocks already
	// accumulated in asst.Content.
	prov := fake.New(fake.Script{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "sure, let me check"},
		{Type: llm.EventToolUseStart, ToolUseID: "call_a", ToolName: "Read"},
		{Type: llm.EventToolUseDelta, ToolUseID: "call_a", PartialJSON: `{"id":"call_a"}`},
		{Type: llm.EventToolUseStop, ToolUseID: "call_a"},
		{Type: llm.EventToolUseStart, ToolUseID: "call_b", ToolName: "Read"},
		{Type: llm.EventToolUseDelta, ToolUseID: "call_b", PartialJSON: `{"id":"call_b"}`},
		{Type: llm.EventToolUseStop, ToolUseID: "call_b"},
	})
	a := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
		Model:    "deepseek-v4-pro",
	})
	a.Start()
	defer a.Close()
	for range a.Submit(context.Background(), "hi") {
	}
	for _, m := range a.history {
		for _, b := range m.Content {
			if b.Type == llm.ContentToolUse {
				t.Fatalf("orphan tool_use leaked into history: id=%q history=%+v", b.ToolUseID, a.history)
			}
		}
	}
}

// Regression: a stream that closes before emitting any block (e.g. the
// user cancels immediately, or the provider hangs up) used to leave an
// assistant message with empty content in history, which then poisoned
// the next turn's wire call on strict validators (DeepSeek).
func TestAgentDropsEmptyAssistantFromHistoryOnEarlyClose(t *testing.T) {
	reg := tools.NewRegistry()
	// Empty script: fake provider closes the channel immediately.
	prov := fake.New(fake.Script{})
	a := New(Options{
		Provider: prov,
		Tools:    reg,
		Policy:   policy.AllowAll(),
		Model:    "deepseek-v4-pro",
	})
	a.Start()
	defer a.Close()
	for range a.Submit(context.Background(), "hello") {
	}
	for _, m := range a.history {
		if m.Role == llm.RoleAssistant && len(m.Content) == 0 {
			t.Fatalf("empty assistant leaked into history: %+v", a.history)
		}
	}
}

func TestAgentMaxTokens_ResolverFiresPerTurn(t *testing.T) {
	reg := tools.NewRegistry()
	prov := fake.New(endTurnScript(), endTurnScript())
	resolver := func(m string) int {
		switch m {
		case "deepseek-v4-pro":
			return 192_000
		default:
			return 0
		}
	}
	a := New(Options{
		Provider:            prov,
		Tools:               reg,
		Policy:              policy.AllowAll(),
		Model:               "claude-sonnet-4-5",
		MaxTokens:           32_768,
		MaxTokensResolverFn: resolver,
	})
	a.Start()
	defer a.Close()

	runOneTurn(t, a, "one")
	if got := prov.Calls[0].MaxTokens; got != 32_768 {
		t.Fatalf("turn1 max_tokens: got %d want 32768", got)
	}

	a.SetModel("deepseek-v4-pro")
	runOneTurn(t, a, "two")
	if got := prov.Calls[1].MaxTokens; got != 192_000 {
		t.Fatalf("turn2 max_tokens: got %d want 192000", got)
	}
}
