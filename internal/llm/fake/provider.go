package fake

import (
	"context"

	"github.com/stefanfaur/sam/internal/llm"
)

// Script is a sequence of events to emit
type Script []llm.StreamEvent

// Provider is a fake LLM provider for testing
type Provider struct {
	scripts []Script
	callIdx int
	Calls   []llm.Request
}

func New(scripts ...Script) *Provider {
	return &Provider{
		scripts: scripts,
		callIdx: 0,
	}
}

func (p *Provider) Name() string { return "fake" }

// LastSystem returns the system prompt of the most recent call, or "" if none.
func (p *Provider) LastSystem() string {
	if len(p.Calls) == 0 {
		return ""
	}
	return p.Calls[len(p.Calls)-1].System
}

func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.Calls = append(p.Calls, req)
	if p.callIdx >= len(p.scripts) {
		// No more scripts - return empty or error stream
		ch := make(chan llm.StreamEvent)
		close(ch)
		return ch, nil
	}

	script := p.scripts[p.callIdx]
	p.callIdx++

	ch := make(chan llm.StreamEvent, len(script))

	// Emit events in a goroutine, respecting context
	go func() {
		defer close(ch)
		for _, ev := range script {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
