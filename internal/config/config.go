package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type ProviderConfig struct {
	BaseURL string `toml:"base_url"`
}

type Config struct {
	Provider         string         `toml:"provider"`
	Model            string         `toml:"model"`
	SystemPromptFile string         `toml:"system_prompt_file"`
	MaxTokens        int            `toml:"max_tokens"`
	MaxIterations    int            `toml:"max_iterations"`
	Providers        struct {
		Minimax   ProviderConfig `toml:"minimax"`
		Anthropic ProviderConfig `toml:"anthropic"`
	} `toml:"providers"`
	TUI struct {
		Theme string `toml:"theme"`
	} `toml:"tui"`
}

// Overrides are CLI flag values that take precedence over the TOML file.
type Overrides struct {
	Provider         string
	Model            string
	SystemPromptFile string
}

func Load(over Overrides) (*Config, error) {
	cfg := &Config{
		Provider:      "minimax",
		MaxTokens:     4096,
		MaxIterations: 25,
	}
	cfg.TUI.Theme = "dark"

	path := getConfigPath()
	if data, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
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

	if cfg.Provider != "minimax" && cfg.Provider != "anthropic" {
		return nil, fmt.Errorf("invalid provider: %s (must be 'minimax' or 'anthropic')", cfg.Provider)
	}
	return cfg, nil
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
