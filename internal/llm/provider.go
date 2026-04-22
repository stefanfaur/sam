package llm

import "context"

type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}