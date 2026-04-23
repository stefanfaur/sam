package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type CapsOverride struct {
	SystemRole                *string `toml:"system_role"`
	SystemRoleFallback        *string `toml:"system_role_fallback"`
	MaxTokensField            *string `toml:"max_tokens_field"`
	SupportsSamplingParams    *bool   `toml:"supports_sampling_params"`
	SupportsReasoningEffort   *bool   `toml:"supports_reasoning_effort"`
	SupportsIncludeUsage      *bool   `toml:"supports_include_usage"`
	SupportsParallelToolCalls *bool   `toml:"supports_parallel_tool_calls"`
	EchoReasoning             *bool   `toml:"echo_reasoning"`
	ReasoningSource           *string `toml:"reasoning_source"`
	AuthHeader                *string `toml:"auth_header"`
}

type ProviderEntry struct {
	Name           string       `toml:"-"`
	Wire           string       `toml:"wire"`
	BaseURL        string       `toml:"base_url"`
	APIKeyEnv      string       `toml:"api_key_env"`
	DefaultModel   string       `toml:"default_model"`
	Models         []string     `toml:"models"`
	ModelPrefixes  []string     `toml:"model_prefixes"`
	ParseThinkTags bool         `toml:"parse_think_tags"`
	Caps           CapsOverride `toml:"caps"`
}

type ModelConfig struct {
	ContextWindow   int    `toml:"context_window"`
	ReasoningEffort string `toml:"reasoning_effort"`
}

type RTKConfig struct {
	Mode string `toml:"mode"` // "auto" (default) | "on" | "off"
}

type Config struct {
	Provider         string                   `toml:"provider"`
	Model            string                   `toml:"model"`
	SystemPromptFile string                   `toml:"system_prompt_file"`
	MaxTokens        int                      `toml:"max_tokens"`
	MaxIterations    int                      `toml:"max_iterations"`
	Providers        map[string]ProviderEntry `toml:"providers"`
	Models           map[string]ModelConfig   `toml:"models"`
	TUI              struct {
		Theme string `toml:"theme"`
	} `toml:"tui"`
	RTK RTKConfig `toml:"rtk"`
}

// ModelContextWindow returns the configured context window in tokens,
// falling back to family defaults or 128k for unknown models.
func (c *Config) ModelContextWindow(name string) int {
	if m, ok := c.Models[name]; ok && m.ContextWindow > 0 {
		return m.ContextWindow
	}
	switch {
	case strings.HasPrefix(name, "claude-opus"),
		strings.HasPrefix(name, "claude-sonnet"),
		strings.HasPrefix(name, "claude-haiku"):
		return 200_000
	case strings.HasPrefix(name, "MiniMax-M2"),
		strings.HasPrefix(name, "MiniMax-M1"):
		return 1_000_000
	case strings.HasPrefix(name, "gpt-5"),
		strings.HasPrefix(name, "o1"),
		strings.HasPrefix(name, "o3"),
		strings.HasPrefix(name, "o4"):
		return 400_000
	case strings.HasPrefix(name, "gpt-4o"),
		strings.HasPrefix(name, "gpt-4.1"):
		return 128_000
	case strings.HasPrefix(name, "deepseek-r"),
		strings.HasPrefix(name, "deepseek-v3"):
		return 131_072
	case strings.HasPrefix(name, "trinity-"):
		return 512_000
	}
	return 128_000
}

// Overrides are CLI flag values that take precedence over the TOML file.
type Overrides struct {
	Provider         string
	Model            string
	SystemPromptFile string
}

// rawConfig mirrors Config but captures the raw presence of the providers
// map so we can full-replace preset entries that the user redeclares.
type rawConfig struct {
	Provider         string                   `toml:"provider"`
	Model            string                   `toml:"model"`
	SystemPromptFile string                   `toml:"system_prompt_file"`
	MaxTokens        int                      `toml:"max_tokens"`
	MaxIterations    int                      `toml:"max_iterations"`
	Providers        map[string]ProviderEntry `toml:"providers"`
	Models           map[string]ModelConfig   `toml:"models"`
	TUI              struct {
		Theme string `toml:"theme"`
	} `toml:"tui"`
	RTK RTKConfig `toml:"rtk"`
}

func Load(over Overrides) (*Config, error) {
	cfg := &Config{
		Provider:      "minimax",
		MaxTokens:     4096,
		MaxIterations: 50,
		Providers:     Presets(),
	}
	cfg.TUI.Theme = "dark"
	cfg.RTK.Mode = "auto"

	path := getConfigPath()
	if data, err := os.ReadFile(path); err == nil {
		var raw rawConfig
		if err := toml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if raw.Provider != "" {
			cfg.Provider = raw.Provider
		}
		if raw.Model != "" {
			cfg.Model = raw.Model
		}
		if raw.SystemPromptFile != "" {
			cfg.SystemPromptFile = raw.SystemPromptFile
		}
		if raw.MaxTokens != 0 {
			cfg.MaxTokens = raw.MaxTokens
		}
		if raw.MaxIterations != 0 {
			cfg.MaxIterations = raw.MaxIterations
		}
		if raw.Models != nil {
			cfg.Models = raw.Models
		}
		if raw.TUI.Theme != "" {
			cfg.TUI.Theme = raw.TUI.Theme
		}
		if raw.RTK.Mode != "" {
			cfg.RTK.Mode = raw.RTK.Mode
		}
		// Full-replace merge: any provider key declared in TOML replaces
		// the preset entry entirely.
		for name, entry := range raw.Providers {
			entry.Name = name
			cfg.Providers[name] = entry
		}
	}

	// Persisted TUI selection overlays config.toml but loses to env + CLI.
	st := LoadState()
	if st.Provider != "" {
		cfg.Provider = st.Provider
	}
	if st.Model != "" {
		cfg.Model = st.Model
	}

	// env (provider + model fallback)
	if v := os.Getenv("SAM_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("SAM_MODEL"); v != "" {
		cfg.Model = v
	}

	// CLI overrides
	if over.Provider != "" {
		cfg.Provider = over.Provider
	}
	if over.Model != "" {
		cfg.Model = over.Model
	}
	if over.SystemPromptFile != "" {
		cfg.SystemPromptFile = over.SystemPromptFile
	}

	// Validate every entry's wire.
	for name, entry := range cfg.Providers {
		switch entry.Wire {
		case "anthropic", "openai":
		default:
			return nil, fmt.Errorf("provider %q: invalid wire %q (must be 'anthropic' or 'openai')", name, entry.Wire)
		}
	}
	if _, ok := cfg.Providers[cfg.Provider]; !ok {
		return nil, fmt.Errorf("unknown provider %q (known: %s)", cfg.Provider, joinProviderNames(cfg.Providers))
	}

	// Validate RTK mode; clamp unknown values to the default.
	switch cfg.RTK.Mode {
	case "auto", "on", "off":
	default:
		cfg.RTK.Mode = "auto"
	}
	return cfg, nil
}

func joinProviderNames(m map[string]ProviderEntry) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}

func MustLoad(over Overrides) *Config {
	cfg, err := Load(over)
	if err != nil {
		panic(err)
	}
	return cfg
}

// LoadSystemPrompt reads SystemPromptFile if set, returning fallback otherwise.
func (c *Config) LoadSystemPrompt(fallback string) string {
	if c.SystemPromptFile == "" {
		return fallback
	}
	b, err := os.ReadFile(c.SystemPromptFile)
	if err != nil {
		return fallback
	}
	return string(b)
}

func getConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sam", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sam", "config.toml")
}
