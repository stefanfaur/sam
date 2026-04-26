package config

import (
	"sort"
	"strings"
)

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
		"claude":            {Prefixes: []string{"claude-opus", "claude-sonnet", "claude-haiku"}},
		"minimax":           {Prefixes: []string{"MiniMax-"}},
		"kimi-k2":           {Prefixes: []string{"kimi-k2"}},
		"trinity":           {Prefixes: []string{"trinity-"}},
		"gpt":               {Prefixes: []string{"gpt-4o", "gpt-4.", "gpt-4-"}},
		"gpt-reasoning":     {Prefixes: []string{"gpt-5", "o1", "o3", "o4"}},
		"deepseek":          {Prefixes: []string{"deepseek-"}},
		"deepseek-reasoner": {Prefixes: []string{"deepseek-reasoner", "deepseek-r1"}},
		"deepseek-v4":       {Prefixes: []string{"deepseek-v4"}},
	}
}

// FamilyForModel returns the family name whose longest prefix matches model,
// using the merged bundled + user PromptFamilies map. Empty when no match.
// Families with len(Prefixes) == 0 are skipped (enables disable-via-empty).
// Ties on prefix length resolve by lexicographic family-name ascending.
func (c *Config) FamilyForModel(model string) string {
	type candidate struct {
		family string
		length int
	}
	var matches []candidate
	for name, fam := range c.PromptFamilies {
		if len(fam.Prefixes) == 0 {
			continue
		}
		for _, p := range fam.Prefixes {
			if p != "" && strings.HasPrefix(model, p) {
				matches = append(matches, candidate{family: name, length: len(p)})
				break
			}
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].length != matches[j].length {
			return matches[i].length > matches[j].length
		}
		return matches[i].family < matches[j].family
	})
	return matches[0].family
}
