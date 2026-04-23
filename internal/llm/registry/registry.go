package registry

import (
	"fmt"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/anthropiccompat"
	"github.com/stefanfaur/sam/internal/llm/openaicompat"
)

// Build routes a provider entry to its wire-specific constructor.
func Build(entry config.ProviderEntry, model string, effortResolver func(string) string, thinkingResolver func(string) int) (llm.Provider, error) {
	switch entry.Wire {
	case "anthropic":
		return anthropiccompat.NewProvider(entry, model, thinkingResolver)
	case "openai":
		return openaicompat.NewProvider(entry, model, effortResolver)
	default:
		return nil, fmt.Errorf("registry: unknown wire %q (provider %q)", entry.Wire, entry.Name)
	}
}
