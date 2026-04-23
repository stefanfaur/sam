package openaicompat

import (
	"context"
	"fmt"
	"os"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
)

// Provider implements llm.Provider against any OpenAI-compatible backend
// described by a config.ProviderEntry.
type Provider struct {
	name   string
	model  string
	client *Client
}

// NewProvider constructs a Provider from a config entry. The entry's wire
// must be "openai". Capabilities derive from DefaultCaps(model) merged with
// the entry's CapsOverride.
func NewProvider(entry config.ProviderEntry, model string, effortResolver func(string) string) (*Provider, error) {
	if entry.Wire != "openai" {
		return nil, fmt.Errorf("openaicompat: wire %q not supported", entry.Wire)
	}
	if model == "" {
		model = entry.DefaultModel
	}
	caps := MergeCaps(DefaultCaps(model), entry.Caps)
	apiKey := ""
	if entry.APIKeyEnv != "" {
		apiKey = os.Getenv(entry.APIKeyEnv)
	}
	// Missing key is surfaced by Stream; construction stays non-fatal so the
	// TUI can boot and let the user run /auth to fill it in.
	client := NewClient(ClientOptions{
		APIKey:         apiKey,
		BaseURL:        entry.BaseURL,
		Caps:           caps,
		ParseThinkTags: entry.ParseThinkTags,
		EffortResolver: effortResolver,
	})
	return &Provider{
		name:   entry.Name,
		model:  model,
		client: client,
	}, nil
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	if req.Model == "" {
		req.Model = p.model
	}
	return p.client.Stream(ctx, req)
}
