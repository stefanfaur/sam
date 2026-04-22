package agent

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

type submit struct {
	userMsg string
	out     chan Event
	ctx     context.Context
}

// Agent orchestrates LLM calls with tool execution
type Agent struct {
	provider  llm.Provider
	tools     *tools.Registry
	policy    *policy.Policy
	system    string
	model     string
	maxTokens int
	maxIters  int
	launchDir string

	history    []llm.Message
	readFiles  map[string]struct{}

	in  chan submit
	mu  sync.Mutex // protects cancelTurn
	cancelTurn context.CancelFunc
	log *slog.Logger
}

type Options struct {
	Provider   llm.Provider
	Tools      *tools.Registry
	Policy     *policy.Policy
	System     string
	Model      string
	MaxTokens  int
	MaxIters   int
	LaunchDir  string
	Logger     *slog.Logger
}

func New(opts Options) *Agent {
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 4096
	}
	if opts.MaxIters == 0 {
		opts.MaxIters = 25
	}
	if opts.LaunchDir == "" {
		cwd, _ := os.Getwd()
		opts.LaunchDir = cwd
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return &Agent{
		provider:   opts.Provider,
		tools:      opts.Tools,
		policy:     opts.Policy,
		system:     opts.System,
		model:      opts.Model,
		maxTokens:  opts.MaxTokens,
		maxIters:   opts.MaxIters,
		launchDir:  opts.LaunchDir,
		readFiles:  make(map[string]struct{}),
		in:         make(chan submit, 1),
		history:    []llm.Message{},
		log:        opts.Logger,
	}
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

// Reset clears history and session allowlist (readFiles stays).
func (a *Agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = []llm.Message{}
	if a.policy != nil {
		a.policy.ResetSession()
	}
}

// SetModel updates the model used for subsequent turns.
func (a *Agent) SetModel(m string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = m
}

// SetProvider swaps the LLM provider for subsequent turns.
func (a *Agent) SetProvider(p llm.Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.provider = p
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
