package minimax

import (
	"context"
	"os"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/anthropiccompat"
)

const defaultBaseURL = "https://api.minimax.io/anthropic"

type Provider struct {
	client *anthropiccompat.Client
	model  string
}

type Options struct {
	APIKey  string
	BaseURL string
	Model   string
}

func New(opts Options) (*Provider, error) {
	if opts.APIKey == "" {
		opts.APIKey = os.Getenv("MINIMAX_API_KEY")
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.Model == "" {
		opts.Model = "MiniMax-M2.7"
	}
	c, err := anthropiccompat.New(anthropiccompat.Options{
		APIKey:  opts.APIKey,
		BaseURL: opts.BaseURL,
	})
	if err != nil {
		return nil, err
	}
	return &Provider{client: c, model: opts.Model}, nil
}

func (p *Provider) Name() string { return "minimax" }

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	if req.Model == "" {
		req.Model = p.model
	}
	return p.client.Stream(ctx, req)
}
