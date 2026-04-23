package config

import "testing"

func TestModelContextWindowDefaults(t *testing.T) {
	c := &Config{}
	cases := map[string]int{
		"claude-opus-4-7":   200_000,
		"claude-sonnet-4-6": 200_000,
		"claude-haiku-4-5":  200_000,
		"MiniMax-M2.7":      1_000_000,
		"MiniMax-M1":        1_000_000,
		"kimi-k2.6":         262_144,
		"kimi-k2.5":         262_144,
		"unknown-xyz":       128_000,
		"":                  128_000,
	}
	for name, want := range cases {
		if got := c.ModelContextWindow(name); got != want {
			t.Errorf("ModelContextWindow(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestModelContextWindowUserOverride(t *testing.T) {
	c := &Config{
		Models: map[string]ModelConfig{
			"claude-sonnet-4-6": {ContextWindow: 50_000},
			"my-local-model":    {ContextWindow: 32_768},
		},
	}
	if got := c.ModelContextWindow("claude-sonnet-4-6"); got != 50_000 {
		t.Errorf("user override failed: got %d", got)
	}
	if got := c.ModelContextWindow("my-local-model"); got != 32_768 {
		t.Errorf("unknown model with user override: got %d", got)
	}
	// override with zero falls back to default
	c.Models["claude-opus-4-7"] = ModelConfig{ContextWindow: 0}
	if got := c.ModelContextWindow("claude-opus-4-7"); got != 200_000 {
		t.Errorf("zero override should fall back: got %d", got)
	}
}
