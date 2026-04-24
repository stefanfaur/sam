package tui

import (
	"context"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

func drainOneTurn(t *testing.T, a *agent.Agent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := a.Submit(ctx, "ping")
	for range ch {
	}
}

func newTestModelWithResolver(t *testing.T, resolved map[string]string) (*Model, *agent.Agent, *fake.Provider, map[string]*fake.Provider) {
	t.Helper()
	startProv := fake.New()
	a := agent.New(agent.Options{
		Provider: startProv,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		System:   "START",
		Model:    "starter",
		MaxIters: 1,
	})
	a.Start()
	t.Cleanup(func() { a.Close() })

	providers := map[string]config.ProviderEntry{
		"anthropic": {Name: "anthropic", Wire: "anthropic", DefaultModel: "claude-sonnet-4-5", ModelPrefixes: []string{"claude-"}},
		"openai":    {Name: "openai", Wire: "openai", DefaultModel: "gpt-4o-mini"},
	}
	built := map[string]*fake.Provider{}
	factory := func(name, model string) (llm.Provider, error) {
		p := fake.New()
		built[name+"/"+model] = p
		return p, nil
	}
	m := New(a, nil, Options{
		Provider:         "anthropic",
		Model:            "claude-sonnet-4-5",
		MaxIter:          1,
		ProviderFactory:  factory,
		ContextWindowFn:  func(_ string) int { return 200_000 },
		SystemResolverFn: func(model string) string { return resolved[model] },
		Providers:        providers,
	})
	m.status.provider = "anthropic"
	m.status.model = "claude-sonnet-4-5"
	return m, a, startProv, built
}

func TestSwitchProvider_UpdatesSystemPrompt(t *testing.T) {
	resolved := map[string]string{
		"claude-sonnet-4-5": "SYS-CLAUDE",
		"gpt-4o-mini":       "SYS-GPT",
	}
	m, a, _, built := newTestModelWithResolver(t, resolved)
	_ = m.switchProvider("openai", "")
	drainOneTurn(t, a)
	prov := built["openai/gpt-4o-mini"]
	if prov == nil {
		t.Fatal("openai provider not built")
	}
	if got := prov.LastSystem(); got != "SYS-GPT" {
		t.Fatalf("system not updated after switchProvider: %q", got)
	}
}

func TestApplyModelSpec_SameProviderUpdatesSystem(t *testing.T) {
	resolved := map[string]string{
		"claude-sonnet-4-5": "SYS-SONNET",
		"claude-opus-4-7":   "SYS-OPUS",
	}
	m, a, startProv, _ := newTestModelWithResolver(t, resolved)
	_ = m.applyModelSpec("claude-opus-4-7")
	drainOneTurn(t, a)
	if got := startProv.LastSystem(); got != "SYS-OPUS" {
		t.Fatalf("system not updated after applyModelSpec: %q", got)
	}
}
