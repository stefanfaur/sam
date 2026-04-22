package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/anthropic"
	"github.com/stefanfaur/sam/internal/llm/minimax"
	"github.com/stefanfaur/sam/internal/logging"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
	"github.com/stefanfaur/sam/internal/tui"
)

const defaultSystemPrompt = `You are SAM, a coding assistant. You have access to tools:
- Read: Read file contents (requires absolute path)
- Write: Write files (requires Read first for existing files)
- Edit: Edit files with exact string replacement
- Bash: Execute shell commands

Use tools when appropriate to fulfill user requests.`

func main() {
	prompt := flag.String("p", "", "one-shot prompt; omit for interactive")
	providerFlag := flag.String("provider", "", "provider override (minimax|anthropic)")
	modelFlag := flag.String("model", "", "model override")
	systemPromptFile := flag.String("system-prompt", "", "path to a file containing the system prompt")
	flag.Parse()

	config.LoadSecrets().ApplyEnv()

	cfg, err := config.Load(config.Overrides{
		Provider:         *providerFlag,
		Model:            *modelFlag,
		SystemPromptFile: *systemPromptFile,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}

	logger, ring, _ := logging.Setup(logging.DefaultStateDir())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *prompt != "" {
		if err := runAgentOneShot(ctx, cfg, *prompt, logger); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	runTUI(ctx, cfg, logger, ring)
}

func mustProvider(cfg *config.Config) llm.Provider {
	p, err := buildProvider(cfg, cfg.Provider, cfg.Model)
	if err != nil {
		panic(err)
	}
	return p
}

func buildProvider(cfg *config.Config, name, model string) (llm.Provider, error) {
	// Reload secrets so /auth saves take effect on rebuild.
	config.LoadSecrets().ApplyEnv()
	switch name {
	case "minimax":
		return minimax.New(minimax.Options{
			BaseURL: cfg.Providers.Minimax.BaseURL,
			Model:   model,
		})
	case "anthropic":
		return anthropic.New(anthropic.Options{
			BaseURL: cfg.Providers.Anthropic.BaseURL,
			Model:   model,
		})
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

func buildRegistry(cwd string) (*tools.Registry, *tools.ReadTracker) {
	tracker := tools.NewReadTracker()
	reg := tools.NewRegistry()
	reg.Register(tools.NewRead(tracker))
	reg.Register(tools.NewWrite(tracker))
	reg.Register(tools.NewEdit(tracker))
	reg.Register(tools.NewBash(cwd))
	return reg, tracker
}

func runTUI(ctx context.Context, cfg *config.Config, logger *slog.Logger, ring *logging.Ring) {
	cwd, _ := os.Getwd()
	registry, _ := buildRegistry(cwd)

	pol := policy.Default()
	prov := mustProvider(cfg)

	sys := cfg.LoadSystemPrompt(defaultSystemPrompt)

	a := agent.New(agent.Options{
		Provider:  prov,
		Tools:     registry,
		Policy:    pol,
		System:    sys,
		Model:     cfg.Model,
		MaxIters:  cfg.MaxIterations,
		MaxTokens: cfg.MaxTokens,
		LaunchDir: cwd,
		Logger:    logger,
	})
	a.Start()
	defer a.Close()

	model := tui.New(a, ring, tui.Options{
		Provider: cfg.Provider,
		Model:    cfg.Model,
		MaxIter:  cfg.MaxIterations,
		ProviderFactory: func(name, modelName string) (llm.Provider, error) {
			return buildProvider(cfg, name, modelName)
		},
		ContextWindowFn: cfg.ModelContextWindow,
	})
	prog := tea.NewProgram(model, tea.WithContext(ctx))
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runAgentOneShot(ctx context.Context, cfg *config.Config, prompt string, logger *slog.Logger) error {
	cwd, _ := os.Getwd()
	registry, _ := buildRegistry(cwd)
	pol := policy.AllowAll()
	prov := mustProvider(cfg)
	sys := cfg.LoadSystemPrompt(defaultSystemPrompt)

	a := agent.New(agent.Options{
		Provider:  prov,
		Tools:     registry,
		Policy:    pol,
		System:    sys,
		Model:     cfg.Model,
		MaxIters:  cfg.MaxIterations,
		MaxTokens: cfg.MaxTokens,
		LaunchDir: cwd,
		Logger:    logger,
	})
	a.Start()
	defer a.Close()

	events := a.Submit(ctx, prompt)
	for ev := range events {
		switch e := ev.(type) {
		case agent.TextDelta:
			fmt.Print(e.Text)
		case agent.ToolCall:
			in := string(e.Input)
			if len(in) > 120 {
				in = in[:120] + "…"
			}
			fmt.Printf("\n[tool:%s %s]\n", e.Name, in)
		case agent.ToolResult:
			for _, l := range head(e.Output, 5) {
				fmt.Printf("  %s\n", l)
			}
		case agent.ErrorEvent:
			fmt.Printf("\n[error: %v]\n", e.Err)
		case agent.TurnDone:
			fmt.Println()
		case agent.ApprovalRequest:
			// --allow-all path means this shouldn't happen, but respond to
			// avoid deadlocks if a tool gets an unknown policy.
			e.Respond(agent.ApprovalDecision{Kind: agent.DecisionDeny})
		}
	}
	return nil
}

func head(s string, n int) []string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		rest := len(lines) - n
		lines = lines[:n]
		lines = append(lines, fmt.Sprintf("… %d more lines", rest))
	}
	return lines
}
