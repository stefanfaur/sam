package config

// PromptFamily groups model-name prefixes that share a per-family system
// prompt addendum. Matching is case-sensitive; declare case variants in
// Prefixes when needed.
type PromptFamily struct {
	Prefixes []string `toml:"prefixes"`
}

// DefaultPromptFamilies returns the bundled family→prefix map. Callers own
// the returned value and may mutate or replace entries. Every call returns
// a fresh copy.
func DefaultPromptFamilies() map[string]PromptFamily {
	return map[string]PromptFamily{
		"claude":   {Prefixes: []string{"claude-opus", "claude-sonnet", "claude-haiku"}},
		"minimax":  {Prefixes: []string{"MiniMax-"}},
		"kimi-k2":  {Prefixes: []string{"kimi-k2"}},
		"trinity":  {Prefixes: []string{"trinity-"}},
		"gpt":      {Prefixes: []string{"gpt-5", "gpt-4o", "gpt-4.1", "o1", "o3", "o4"}},
		"deepseek": {Prefixes: []string{"deepseek-"}},
	}
}
