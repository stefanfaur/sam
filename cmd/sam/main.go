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
	"github.com/stefanfaur/sam/internal/llm/registry"
	"github.com/stefanfaur/sam/internal/logging"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/rtk"
	"github.com/stefanfaur/sam/internal/skills"
	"github.com/stefanfaur/sam/internal/system"
	"github.com/stefanfaur/sam/internal/tools"
	"github.com/stefanfaur/sam/internal/tui"
)

func main() {
	prompt := flag.String("p", "", "one-shot prompt; omit for interactive")
	providerFlag := flag.String("provider", "", "provider override (minimax|anthropic)")
	modelFlag := flag.String("model", "", "model override")
	systemPromptFile := flag.String("system-prompt", "", "path to a file containing the system prompt")
	flag.Parse()

	cfg, err := config.Load(config.Overrides{
		Provider:         *providerFlag,
		Model:            *modelFlag,
		SystemPromptFile: *systemPromptFile,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}

	config.LoadSecrets().ApplyEnv(cfg)

	logger, ring, _ := logging.Setup(logging.DefaultStateDir())

	sysDir := system.DefaultDir()
	if err := system.Seed(sysDir, logger); err != nil {
		logger.Warn("system seed failed", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rtkClient := rtk.New(rtk.Mode(cfg.RTK.Mode))
	if err := rtkClient.Detect(ctx); err != nil {
		logger.Error("rtk detection failed", "mode", cfg.RTK.Mode, "err", err)
		fmt.Fprintln(os.Stderr, "rtk error:", err)
		os.Exit(2)
	}
	if rtkClient.Enabled() {
		logger.Info("rtk enabled", "version", rtkClient.Version(), "mode", cfg.RTK.Mode)
	} else {
		logger.Info("rtk disabled", "mode", cfg.RTK.Mode)
	}

	if *prompt != "" {
		if err := runAgentOneShot(ctx, cfg, sysDir, *prompt, logger, rtkClient); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	runTUI(ctx, cfg, sysDir, logger, ring, rtkClient)
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
	config.LoadSecrets().ApplyEnv(cfg)
	entry, ok := cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	if model == "" {
		model = entry.DefaultModel
	}
	resolver := func(m string) string {
		if e := cfg.Models[m].ReasoningEffort; e != "" {
			return e
		}
		return config.DefaultReasoningEffort(m)
	}
	thinkingResolver := func(m string) int { return cfg.ModelThinkingBudget(m) }
	return registry.Build(entry, model, resolver, thinkingResolver)
}

func buildRegistry(cwd, sysDir string, rtkClient *rtk.Client) (*tools.Registry, *tools.ReadTracker) {
	desc := func(name string) string {
		s, _ := system.LoadToolDescription(sysDir, name)
		if s == "" {
			s = system.EmbeddedToolDescription(name)
		}
		return s
	}
	tracker := tools.NewReadTracker()
	reg := tools.NewRegistry()
	reg.Register(tools.NewRead(tracker, rtkClient, desc("Read")))
	reg.Register(tools.NewWrite(tracker, desc("Write")))
	reg.Register(tools.NewEdit(tracker, desc("Edit")))
	reg.Register(tools.NewBash(cwd, rtkClient, desc("Bash")))
	return reg, tracker
}

// resolveSystemPrompt builds the effective system prompt for a given model.
// cfg.SystemPromptFile (CLI / config total-override) short-circuits family
// composition. Otherwise base (disk > embedded) is joined with the family
// addendum (disk > embedded) via a single blank line.
func resolveSystemPrompt(cfg *config.Config, sysDir, model string, logger *slog.Logger) string {
	if cfg.SystemPromptFile != "" {
		if s := cfg.LoadSystemPrompt(""); s != "" {
			return s
		}
	}
	base, _ := system.LoadSystemPrompt(sysDir)
	if base == "" {
		base = system.EmbeddedPrompt()
	}
	family := cfg.FamilyForModel(model)
	if family == "" {
		if logger != nil {
			logger.Info("system: family resolved", "model", model, "family", "", "source", "none")
		}
		return base
	}
	var add, source string
	if system.FamilyPromptExists(sysDir, family) {
		add, _ = system.LoadFamilyPrompt(sysDir, family)
		source = "disk"
	} else {
		add = system.EmbeddedFamilyPrompt(family)
		source = "embedded"
	}
	if add == "" {
		if logger != nil {
			logger.Debug("system: family resolved with no content", "family", family, "source", source)
		}
		return base
	}
	if logger != nil {
		logger.Info("system: family resolved", "model", model, "family", family, "source", source)
	}
	return strings.TrimRight(base, "\n\t ") + "\n\n" + strings.TrimRight(add, "\n\t ")
}

func runTUI(ctx context.Context, cfg *config.Config, sysDir string, logger *slog.Logger, ring *logging.Ring, rtkClient *rtk.Client) {
	cwd, _ := os.Getwd()
	registry, _ := buildRegistry(cwd, sysDir, rtkClient)

	pol := policy.Default()
	prov := mustProvider(cfg)

	sys := resolveSystemPrompt(cfg, sysDir, cfg.Model, logger)

	skillReg := buildSkillsRegistry(cwd, logger)

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
		Skills:    skillReg,
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
		SystemResolverFn: func(m string) string {
			return resolveSystemPrompt(cfg, sysDir, m, logger)
		},
		Providers: cfg.Providers,
	})
	model.SetSkills(skillReg)
	prog := tea.NewProgram(model, tea.WithContext(ctx))
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func buildSkillsRegistry(cwd string, logger *slog.Logger) *skills.Registry {
	overrides, err := skills.LoadOverrides()
	if err != nil {
		logger.Warn("skills: load overrides failed", "err", err)
	}
	trust := skills.NewTrustList(overrides.Trust, overrides.Deny)

	home, _ := os.UserHomeDir()
	var rawRoots []string
	if len(overrides.SkillRoots) > 0 {
		rawRoots = overrides.SkillRoots
	} else {
		rawRoots = []string{".sam/skills", "~/.sam/skills", "~/.agents/skills"}
	}
	roots := skills.ExpandRoots(rawRoots, cwd, home)

	reg := skills.NewRegistry(roots, overrides, trust, tui.BuiltinNames, logger)
	if err := reg.Load(); err != nil {
		logger.Warn("skills: load failed", "err", err)
	}
	return reg
}

func runAgentOneShot(ctx context.Context, cfg *config.Config, sysDir, prompt string, logger *slog.Logger, rtkClient *rtk.Client) error {
	cwd, _ := os.Getwd()
	registry, _ := buildRegistry(cwd, sysDir, rtkClient)
	pol := policy.AllowAll()
	prov := mustProvider(cfg)
	sys := resolveSystemPrompt(cfg, sysDir, cfg.Model, logger)

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
