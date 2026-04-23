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
	// Rewritten is set by tools that optionally replace the input with a
	// compressed equivalent (Bash via rtk rewrite). Empty when no rewrite
	// occurred. Surfaced to the TUI for display; not sent to the LLM.
	Rewritten string
}
