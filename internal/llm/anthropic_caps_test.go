package llm

import "testing"

func TestAnthropicVisionSupported(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-3-opus", true},
		{"claude-3-sonnet-20240229", true},
		{"claude-3-haiku-20240307", true},
		{"claude-3-5-sonnet-20241022", true},
		{"claude-sonnet-4-5", true},
		{"claude-opus-4-5", true},
		{"claude-haiku-4-5", true},
		{"unknown-model", false},
		{"deepseek-v3", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := AnthropicVisionSupported(tc.model); got != tc.want {
			t.Errorf("AnthropicVisionSupported(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}
