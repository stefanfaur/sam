package openaicompat

import (
	"testing"

	"github.com/stefanfaur/sam/internal/config"
)

func ptrS(s string) *string { return &s }
func ptrB(b bool) *bool     { return &b }

func TestDefaultCapsPrefixMatch(t *testing.T) {
	cases := map[string]struct {
		role   string
		field  string
		effort bool
		src    string
		echo   bool
	}{
		"gpt-5":                  {"developer", "max_completion_tokens", true, "none", false},
		"o3-mini":                {"developer", "max_completion_tokens", true, "none", false},
		"gpt-4o":                 {"system", "max_tokens", false, "none", false},
		"gpt-4.1":                {"system", "max_tokens", false, "none", false},
		"deepseek-r1":            {"system", "max_tokens", false, "reasoning_content", true},
		"trinity-large-thinking": {"system", "max_tokens", false, "reasoning_content", true},
		"llama-3.3-70b":          {"system", "max_tokens", false, "none", false},
	}
	for model, want := range cases {
		c := DefaultCaps(model)
		if c.SystemRole != want.role {
			t.Errorf("%s role: %q", model, c.SystemRole)
		}
		if c.MaxTokensField != want.field {
			t.Errorf("%s field: %q", model, c.MaxTokensField)
		}
		if c.SupportsReasoningEffort != want.effort {
			t.Errorf("%s effort: %v", model, c.SupportsReasoningEffort)
		}
		if c.ReasoningSource != want.src {
			t.Errorf("%s src: %q", model, c.ReasoningSource)
		}
		if c.EchoReasoning != want.echo {
			t.Errorf("%s echo: %v", model, c.EchoReasoning)
		}
		if !c.SupportsIncludeUsage || !c.SupportsParallelToolCalls || c.AuthHeader != "bearer" {
			t.Errorf("%s universal defaults not overlaid: %+v", model, c)
		}
	}
}

func TestMergeCapsOverridesReplace(t *testing.T) {
	base := DefaultCaps("gpt-4o")
	ov := config.CapsOverride{
		SystemRole:             ptrS("developer"),
		MaxTokensField:         ptrS("max_completion_tokens"),
		SupportsSamplingParams: ptrB(false),
		EchoReasoning:          ptrB(true),
		AuthHeader:             ptrS("api-key"),
	}
	got := MergeCaps(base, ov)
	if got.SystemRole != "developer" || got.MaxTokensField != "max_completion_tokens" {
		t.Errorf("role/field: %+v", got)
	}
	if got.SupportsSamplingParams {
		t.Error("sampling should be off")
	}
	if !got.EchoReasoning {
		t.Error("echo should be on")
	}
	if got.AuthHeader != "api-key" {
		t.Errorf("auth: %q", got.AuthHeader)
	}
	// Unset overrides preserve base.
	if got.SupportsIncludeUsage != base.SupportsIncludeUsage {
		t.Error("unset override clobbered include_usage")
	}
}

func TestMatchesPrefix(t *testing.T) {
	if !MatchesPrefix("gpt-5") {
		t.Error("gpt-5 should match")
	}
	if !MatchesPrefix("trinity-large-thinking") {
		t.Error("trinity should match")
	}
	if MatchesPrefix("claude-sonnet-4-5") {
		t.Error("claude should not match openai caps")
	}
	if MatchesPrefix("MiniMax-M2.7") {
		t.Error("minimax should not match")
	}
}
