package tools

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Run(ctx context.Context, raw json.RawMessage) (Result, error)
}

type Result struct {
	Output  string
	IsError bool
}