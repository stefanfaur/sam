package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Secrets struct {
	MinimaxAPIKey   string `toml:"minimax_api_key,omitempty"`
	AnthropicAPIKey string `toml:"anthropic_api_key,omitempty"`
}

func SecretsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sam", "secrets.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sam", "secrets.toml")
}

func LoadSecrets() Secrets {
	var s Secrets
	path := SecretsPath()
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = toml.Unmarshal(b, &s)
	return s
}

func SaveSecrets(s Secrets) error {
	path := SecretsPath()
	if path == "" {
		return fmt.Errorf("cannot resolve secrets path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(s)
}

// ApplyEnv exports secrets to env vars when they are not already set.
// Returns the Secrets struct for chained use.
func (s Secrets) ApplyEnv() Secrets {
	if s.MinimaxAPIKey != "" && os.Getenv("MINIMAX_API_KEY") == "" {
		_ = os.Setenv("MINIMAX_API_KEY", s.MinimaxAPIKey)
	}
	if s.AnthropicAPIKey != "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		_ = os.Setenv("ANTHROPIC_API_KEY", s.AnthropicAPIKey)
	}
	return s
}
