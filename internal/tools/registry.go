package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/stefanfaur/sam/internal/llm"
)

type typedBase struct {
	name         string
	desc         string
	sch          map[string]any
	parallelSafe bool
}

type typed[In any] struct {
	typedBase
	run func(context.Context, In) (Result, error)
}

type Option func(*typedBase)

func ParallelSafe() Option {
	return func(b *typedBase) { b.parallelSafe = true }
}

func New[In any](
	name, desc string,
	fn func(context.Context, In) (Result, error),
	opts ...Option,
) Tool {
	t := &typed[In]{
		typedBase: typedBase{name: name, desc: desc, sch: SchemaOf[In]()},
		run:       fn,
	}
	for _, o := range opts {
		o(&t.typedBase)
	}
	return t
}

func (t *typed[In]) Name() string           { return t.name }
func (t *typed[In]) Description() string    { return t.desc }
func (t *typed[In]) Schema() map[string]any { return t.sch }
func (t *typed[In]) ParallelSafe() bool     { return t.parallelSafe }

func (t *typed[In]) Run(ctx context.Context, raw json.RawMessage) (res Result, err error) {
	var in In
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return Result{Output: "invalid input: " + err.Error(), IsError: true}, nil
		}
	}
	defer func() {
		if r := recover(); r != nil {
			res = Result{
				Output:  fmt.Sprintf("panic in tool %s: %v", t.name, r),
				IsError: true,
			}
			err = nil
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
