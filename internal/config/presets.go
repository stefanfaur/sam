package config

// Presets returns the bundled provider entries. Callers own the returned map
// and may mutate or replace entries. Every call returns a fresh copy.
func Presets() map[string]ProviderEntry {
	return map[string]ProviderEntry{
		"minimax": {
			Name:         "minimax",
			Wire:         "anthropic",
			BaseURL:      "https://api.minimax.io/anthropic",
			APIKeyEnv:    "MINIMAX_API_KEY",
			DefaultModel: "MiniMax-M2.7",
		},
		"anthropic": {
			Name:         "anthropic",
			Wire:         "anthropic",
			BaseURL:      "",
			APIKeyEnv:    "ANTHROPIC_API_KEY",
			DefaultModel: "claude-sonnet-4-5",
		},
		"openai": {
			Name:         "openai",
			Wire:         "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKeyEnv:    "OPENAI_API_KEY",
			DefaultModel: "gpt-4o-mini",
		},
		"arcee": {
			Name:         "arcee",
			Wire:         "openai",
			BaseURL:      "https://api.arcee.ai/api/v1",
			APIKeyEnv:    "ARCEE_API_KEY",
			DefaultModel: "trinity-large-thinking",
			// §0 probe pending: Conductor default assumes reasoning_content
			// extraction server-side; flip to true if probe finds inline <think>.
			ParseThinkTags: false,
		},
		"moonshot": {
			Name:         "moonshot",
			Wire:         "openai",
			BaseURL:      "https://api.moonshot.ai/v1",
			APIKeyEnv:    "KIMI_API_KEY",
			DefaultModel: "kimi-k2.6",
		},
	}
}
