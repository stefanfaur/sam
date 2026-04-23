package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Secrets holds per-provider API keys keyed by provider name (matching
// cfg.Providers[name]).
type Secrets struct {
	APIKeys map[string]string `toml:"api_keys"`
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
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
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

// ApplyEnv exports per-provider secrets into the env var declared by each
// provider entry's APIKeyEnv, unless the env var is already set.
func (s Secrets) ApplyEnv(cfg *Config) Secrets {
	if cfg == nil {
		return s
	}
	for name, entry := range cfg.Providers {
		key := s.APIKeys[name]
		if key == "" || entry.APIKeyEnv == "" {
			continue
		}
		if os.Getenv(entry.APIKeyEnv) != "" {
			continue
		}
		_ = os.Setenv(entry.APIKeyEnv, key)
	}
	return s
}
