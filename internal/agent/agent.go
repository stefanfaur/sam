package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/skills"
	"github.com/stefanfaur/sam/internal/tools"
)

type submit struct {
	userMsg string
	out     chan Event
	ctx     context.Context
}

// Agent orchestrates LLM calls with tool execution
type Agent struct {
	provider          llm.Provider
	tools             *tools.Registry
	policy            *policy.Policy
	baseSystem        string
	system            string
	catalog           string
	model             string
	maxTokens         int
	maxTokensResolver func(string) int
	maxIters          int
	launchDir         string

	history []llm.Message

	skills *skills.Registry

	in         chan submit
	mu         sync.Mutex // protects cancelTurn + system/catalog + skills pointer
	cancelTurn context.CancelFunc
	log        *slog.Logger
}

type Options struct {
	Provider            llm.Provider
	Tools               *tools.Registry
	Policy              *policy.Policy
	System              string
	Model               string
	MaxTokens           int
	MaxTokensResolverFn func(model string) int
	MaxIters            int
	LaunchDir           string
	Logger              *slog.Logger
	Skills              *skills.Registry
}

func New(opts Options) *Agent {
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 32768
	}
	if opts.MaxIters == 0 {
		opts.MaxIters = 50
	}
	if opts.LaunchDir == "" {
		cwd, _ := os.Getwd()
		opts.LaunchDir = cwd
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	a := &Agent{
		provider:          opts.Provider,
		tools:             opts.Tools,
		policy:            opts.Policy,
		baseSystem:        opts.System,
		system:            opts.System,
		model:             opts.Model,
		maxTokens:         opts.MaxTokens,
		maxTokensResolver: opts.MaxTokensResolverFn,
		maxIters:          opts.MaxIters,
		launchDir:         opts.LaunchDir,
		skills:            opts.Skills,
		in:                make(chan submit, 1),
		history:           []llm.Message{},
		log:               opts.Logger,
	}
	a.RebuildSkillCatalog()
	return a
}

// SetSkills swaps the skills registry and rebuilds the catalog.
func (a *Agent) SetSkills(r *skills.Registry) {
	a.mu.Lock()
	a.skills = r
	a.mu.Unlock()
	a.RebuildSkillCatalog()
}

// Skills returns the current registry (may be nil).
func (a *Agent) Skills() *skills.Registry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.skills
}

// RebuildSkillCatalog rebuilds the system prompt with (or without) the
// model-invocable catalog depending on the registry + auto-invoke gate.
func (a *Agent) RebuildSkillCatalog() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.skills == nil {
		a.catalog = ""
		a.system = a.baseSystem
		return
	}
	ov := a.skills.Overrides()
	gateOn := ov.AutoInvokeEnable != nil && *ov.AutoInvokeEnable
	if !gateOn {
		a.catalog = ""
		a.system = a.baseSystem
		return
	}
	cr := a.skills.Catalog()
	a.catalog = cr.Block
	if a.catalog == "" {
		a.system = a.baseSystem
	} else if a.baseSystem == "" {
		a.system = a.catalog
	} else {
		a.system = a.baseSystem + "\n\n" + a.catalog
	}
	if cr.TokensEstimate > skills.CatalogSoftCapTokens {
		a.log.Warn("skills: catalog exceeds soft cap", "tokens", cr.TokensEstimate)
	}
}

// SubmitSkill resolves a skill by its slash name (or namespaced form),
// renders its body with $ARGUMENTS substitution, and submits it as a normal
// user turn. The first event on the returned channel is SkillInvoked so the
// TUI can decorate the history row.
func (a *Agent) SubmitSkill(ctx context.Context, nameOrNS, args string) (<-chan Event, error) {
	a.mu.Lock()
	reg := a.skills
	a.mu.Unlock()
	if reg == nil {
		return nil, fmt.Errorf("skills: registry not configured")
	}
	sk, ok := reg.Resolve(nameOrNS)
	if !ok {
		return nil, fmt.Errorf("unknown skill: %s", nameOrNS)
	}
	if !sk.Enabled || !sk.UserInvocable {
		return nil, fmt.Errorf("skill disabled: %s", sk.Name)
	}
	body, err := skills.RenderInvocation(sk, args)
	if err != nil {
		return nil, err
	}
	header := "/" + nameOrNS
	if args = trimArgs(args); args != "" {
		header += " " + args
	}
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		emitToChan(out, SkillInvoked{
			Fingerprint: string(sk.Fingerprint),
			Header:      header,
			Source:      string(sk.Source),
			Body:        body,
		}, ctx)
		src := a.Submit(ctx, body)
		for e := range src {
			emitToChan(out, e, ctx)
		}
	}()
	return out, nil
}

func trimArgs(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

// Start launches the agent goroutine
func (a *Agent) Start() {
	go a.run()
}

// Close shuts down the agent
func (a *Agent) Close() {
	close(a.in)
}

// Submit sends a message to the agent for processing
func (a *Agent) Submit(ctx context.Context, userMsg string) <-chan Event {
	out := make(chan Event, 64)
	a.in <- submit{userMsg: userMsg, out: out, ctx: ctx}
	return out
}

// CancelCurrent cancels the currently executing turn
func (a *Agent) CancelCurrent() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelTurn != nil {
		a.cancelTurn()
	}
}

// LaunchDir returns the directory the agent was launched from
func (a *Agent) LaunchDir() string {
	return a.launchDir
}

// Reset clears history and session allowlist.
func (a *Agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = []llm.Message{}
	if a.policy != nil {
		a.policy.ResetSession()
	}
}

// ClearHistory drops conversation history while preserving the session
// allowlist. Use for /clear; use Reset for a full reset.
func (a *Agent) ClearHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = []llm.Message{}
}

// SetModel updates the model used for subsequent turns.
func (a *Agent) SetModel(m string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = m
}

// SetMaxIters updates the per-turn tool iteration cap. Non-positive values are ignored.
func (a *Agent) SetMaxIters(n int) {
	if n <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.maxIters = n
}

// SetProvider swaps the LLM provider for subsequent turns.
func (a *Agent) SetProvider(p llm.Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.provider = p
}

// SetSystem swaps the base system prompt and rebuilds the effective prompt
// (base + skill catalog, when the auto-invoke gate is on) for subsequent
// turns. Safe to call concurrently with turn execution.
func (a *Agent) SetSystem(s string) {
	a.mu.Lock()
	a.baseSystem = s
	a.mu.Unlock()
	a.RebuildSkillCatalog()
}

// Provider returns the current provider name.
func (a *Agent) ProviderName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider == nil {
		return ""
	}
	return a.provider.Name()
}

func (a *Agent) run() {
	for s := range a.in {
		ctx, cancel := context.WithCancel(s.ctx)
		a.mu.Lock()
		a.cancelTurn = cancel
		a.mu.Unlock()

		a.turn(ctx, s)

		a.mu.Lock()
		a.cancelTurn = nil
		a.mu.Unlock()
		close(s.out)
	}
}
