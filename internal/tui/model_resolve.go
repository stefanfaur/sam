package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm/openaicompat"
)

// resolveModelSpec implements R3 hybrid routing for /model:
//   - `provider/model` explicit form → exact split, validate provider exists.
//   - bare model name → scan providers, matching priority:
//     1. exact DefaultModel
//     2. Models membership
//     3. ModelPrefixes match OR hardcoded openaicompat prefix table match
//
// Zero matches return an error; multiple return a disambiguation hint.
func resolveModelSpec(spec string, providers map[string]config.ProviderEntry) (provider, model string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("model spec empty")
	}
	if idx := strings.Index(spec, "/"); idx >= 0 {
		p := spec[:idx]
		m := spec[idx+1:]
		if _, ok := providers[p]; !ok {
			return "", "", fmt.Errorf("unknown provider %q", p)
		}
		return p, m, nil
	}
	names := sortedProviderNames(providers)
	var matches []string
	for _, name := range names {
		entry := providers[name]
		if entry.DefaultModel == spec {
			matches = append(matches, name)
			continue
		}
		if containsStr(entry.Models, spec) {
			matches = append(matches, name)
			continue
		}
		if anyPrefix(entry.ModelPrefixes, spec) {
			matches = append(matches, name)
			continue
		}
		// Openai wire: hardcoded prefix table covers gpt-5, o-series, etc.
		if entry.Wire == "openai" && openaicompat.MatchesPrefix(spec) {
			matches = append(matches, name)
		}
	}
	// De-duplicate while preserving order.
	matches = dedup(matches)
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("unknown model %q; try provider/model", spec)
	case 1:
		return matches[0], spec, nil
	default:
		return "", "", fmt.Errorf("ambiguous model %q; matches: %s; use provider/model",
			spec, strings.Join(matches, ", "))
	}
}

func sortedProviderNames(m map[string]config.ProviderEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func anyPrefix(prefixes []string, s string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func dedup(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
