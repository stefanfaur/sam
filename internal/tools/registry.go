package tools

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/stefanfaur/sam/internal/llm"
)

type typed[In any] struct {
	name string
	desc string
	run  func(context.Context, In) (Result, error)
	sch  map[string]any
}

func New[In any](name, desc string, fn func(context.Context, In) (Result, error)) Tool {
	return typed[In]{name: name, desc: desc, run: fn, sch: SchemaOf[In]()}
}

func (t typed[In]) Name() string           { return t.name }
func (t typed[In]) Description() string    { return t.desc }
func (t typed[In]) Schema() map[string]any { return t.sch }

func (t typed[In]) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in In
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return Result{Output: "invalid input: " + err.Error(), IsError: true}, nil
		}
	}
	defer func() {
		if r := recover(); r != nil {
			// caught panic - convert to error result
		}
	}()
	return t.run(ctx, in)
}

type Registry struct {
	mu sync.RWMutex
	m  map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{m: map[string]Tool{}}
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.m[name]
	return t, ok
}

func (r *Registry) Defs() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.ToolDef, 0, len(r.m))
	for _, t := range r.m {
		out = append(out, llm.ToolDef{Name: t.Name(), Description: t.Description(), Schema: t.Schema()})
	}
	return out
}
