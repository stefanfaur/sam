package anthropiccompat

import (
	"context"
	"fmt"
	"os"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
)

// Provider implements llm.Provider against any Anthropic-compatible backend
// described by a config.ProviderEntry (Anthropic, Minimax, and others).
type Provider struct {
	name             string
	model            string
	client           *Client
	thinkingResolver func(string) int
}

// NewProvider constructs a Provider from a config entry. The entry's wire
// must be "anthropic". thinkingResolver supplies the per-model extended
// thinking budget (0 = disabled); nil resolver disables thinking for all.
func NewProvider(entry config.ProviderEntry, model string, thinkingResolver func(string) int) (*Provider, error) {
	if entry.Wire != "anthropic" {
		return nil, fmt.Errorf("anthropiccompat: wire %q not supported", entry.Wire)
	}
	if model == "" {
		model = entry.DefaultModel
	}
	apiKey := ""
	if entry.APIKeyEnv != "" {
		apiKey = os.Getenv(entry.APIKeyEnv)
	}
	// Missing key is surfaced by Stream; construction stays non-fatal so the
	// TUI can boot and let the user run /auth to fill it in.
	c, err := New(Options{APIKey: apiKey, BaseURL: entry.BaseURL})
	if err != nil {
		return nil, err
	}
	return &Provider{name: entry.Name, model: model, client: c, thinkingResolver: thinkingResolver}, nil
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	if req.Model == "" {
		req.Model = p.model
	}
	if req.ThinkingBudgetTokens == 0 && p.thinkingResolver != nil {
		req.ThinkingBudgetTokens = p.thinkingResolver(req.Model)
	}
	return p.client.Stream(ctx, req)
}
