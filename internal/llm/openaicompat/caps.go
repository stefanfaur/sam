package openaicompat

import (
	"strings"

	"github.com/stefanfaur/sam/internal/config"
)

// Capabilities captures per-model quirks of an OpenAI-compatible endpoint:
// role names, token-limit field, whether sampling params are honored, how
// reasoning is surfaced on the wire, and which auth scheme is expected.
type Capabilities struct {
	SystemRole                string
	SystemRoleFallback        string
	MaxTokensField            string // "max_tokens" | "max_completion_tokens"
	SupportsSamplingParams    bool
	SupportsReasoningEffort   bool
	SupportsIncludeUsage      bool
	SupportsParallelToolCalls bool
	EchoReasoning             bool
	ReasoningSource           string // "none" | "inline_think" | "reasoning_content" | "reasoning" | "both"
	AuthHeader                string // "bearer" | "api-key" | "none"
	PrependFormatting         bool   // o-series / gpt-5: prepend "Formatting re-enabled.\n"
}

type capsEntry struct {
	prefixes []string
	caps     Capabilities
}

// orderedCapsTable is scanned in order; longest-prefix matching is emulated
// by listing more-specific prefixes first.
var orderedCapsTable = []capsEntry{
	{
		prefixes: []string{"gpt-5", "o1", "o3", "o4"},
		caps: Capabilities{
			SystemRole:              "developer",
			MaxTokensField:          "max_completion_tokens",
			SupportsSamplingParams:  false,
			SupportsReasoningEffort: true,
			ReasoningSource:         "none",
			EchoReasoning:           false,
			PrependFormatting:       true,
		},
	},
	{
		prefixes: []string{"deepseek-r"},
		caps: Capabilities{
			SystemRole:             "system",
			MaxTokensField:         "max_tokens",
			SupportsSamplingParams: true,
			ReasoningSource:        "reasoning_content",
			EchoReasoning:          true,
		},
	},
	{
		prefixes: []string{"kimi-k2"},
		caps: Capabilities{
			SystemRole:              "system",
			MaxTokensField:          "max_tokens",
			SupportsSamplingParams:  true,
			SupportsReasoningEffort: true,
			ReasoningSource:         "reasoning_content",
			EchoReasoning:           true,
		},
	},
	{
		prefixes: []string{"trinity-large-thinking", "trinity-"},
		caps: Capabilities{
			SystemRole:             "system",
			MaxTokensField:         "max_tokens",
			SupportsSamplingParams: true,
			// §0 probe pending — default to reasoning_content (vLLM
			// deepseek_r1 reasoning-parser output); flip entry caps to
			// "inline_think" if probe finds raw <think> tags in content.
			ReasoningSource: "reasoning_content",
			EchoReasoning:   true,
		},
	},
	{
		prefixes: []string{"gpt-4o", "gpt-4.1", "gpt-4", "gpt-3.5"},
		caps: Capabilities{
			SystemRole:             "system",
			MaxTokensField:         "max_tokens",
			SupportsSamplingParams: true,
			ReasoningSource:        "none",
			EchoReasoning:          false,
		},
	},
}

var defaultBaseCaps = Capabilities{
	SystemRole:                "system",
	MaxTokensField:            "max_tokens",
	SupportsSamplingParams:    true,
	SupportsReasoningEffort:   false,
	SupportsIncludeUsage:      true,
	SupportsParallelToolCalls: true,
	EchoReasoning:             false,
	ReasoningSource:           "none",
	AuthHeader:                "bearer",
}

// DefaultCaps returns the baseline capabilities for a model name, selected by
// the first matching prefix in the table. Universal defaults (include_usage,
// parallel_tool_calls, auth header) are overlaid last.
func DefaultCaps(model string) Capabilities {
	caps := defaultBaseCaps
	for _, e := range orderedCapsTable {
		for _, pfx := range e.prefixes {
			if strings.HasPrefix(model, pfx) {
				merged := caps
				merged.SystemRole = e.caps.SystemRole
				merged.SystemRoleFallback = e.caps.SystemRoleFallback
				merged.MaxTokensField = e.caps.MaxTokensField
				merged.SupportsSamplingParams = e.caps.SupportsSamplingParams
				merged.SupportsReasoningEffort = e.caps.SupportsReasoningEffort
				merged.ReasoningSource = e.caps.ReasoningSource
				merged.EchoReasoning = e.caps.EchoReasoning
				merged.PrependFormatting = e.caps.PrependFormatting
				return merged
			}
		}
	}
	return caps
}

// MatchesPrefix reports whether the model name matches any prefix in the
// capability table. Used by the /model command's R3 routing to disambiguate
// bare model names across providers.
func MatchesPrefix(model string) bool {
	for _, e := range orderedCapsTable {
		for _, pfx := range e.prefixes {
			if strings.HasPrefix(model, pfx) {
				return true
			}
		}
	}
	return false
}

// MergeCaps applies a CapsOverride on top of base. Each non-nil override
// field replaces the corresponding base field verbatim.
func MergeCaps(base Capabilities, ov config.CapsOverride) Capabilities {
	if ov.SystemRole != nil {
		base.SystemRole = *ov.SystemRole
	}
	if ov.SystemRoleFallback != nil {
		base.SystemRoleFallback = *ov.SystemRoleFallback
	}
	if ov.MaxTokensField != nil {
		base.MaxTokensField = *ov.MaxTokensField
	}
	if ov.SupportsSamplingParams != nil {
		base.SupportsSamplingParams = *ov.SupportsSamplingParams
	}
	if ov.SupportsReasoningEffort != nil {
		base.SupportsReasoningEffort = *ov.SupportsReasoningEffort
	}
	if ov.SupportsIncludeUsage != nil {
		base.SupportsIncludeUsage = *ov.SupportsIncludeUsage
	}
	if ov.SupportsParallelToolCalls != nil {
		base.SupportsParallelToolCalls = *ov.SupportsParallelToolCalls
	}
	if ov.EchoReasoning != nil {
		base.EchoReasoning = *ov.EchoReasoning
	}
	if ov.ReasoningSource != nil {
		base.ReasoningSource = *ov.ReasoningSource
	}
	if ov.AuthHeader != nil {
		base.AuthHeader = *ov.AuthHeader
	}
	return base
}
