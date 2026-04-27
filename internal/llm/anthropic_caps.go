package llm

import "strings"

// AnthropicCapabilities mirrors the openaicompat.Capabilities subset that's
// relevant for the native Anthropic SDK wire. Vision is the only flag we need
// today; further fields can be added as concerns appear.
type AnthropicCapabilities struct {
	Vision bool
}

// anthropicVisionPrefixes lists model name prefixes that support vision via
// the Anthropic Messages API. All Claude 3.x and Claude 4.x families accept
// image content blocks.
var anthropicVisionPrefixes = []string{
	"claude-opus",
	"claude-sonnet",
	"claude-haiku",
	"claude-3-opus",
	"claude-3-sonnet",
	"claude-3-haiku",
	"claude-3.5-sonnet",
	"claude-3.5-haiku",
	"claude-3-5-sonnet",
	"claude-3-5-haiku",
	"claude-3-7-sonnet",
}

// AnthropicVisionSupported reports whether an Anthropic-wire model accepts
// image content blocks. Matching is by longest prefix.
func AnthropicVisionSupported(model string) bool {
	model = strings.ToLower(model)
	for _, p := range anthropicVisionPrefixes {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}
