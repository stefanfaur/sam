package config

import (
	"os"
	"testing"
)

func TestApplyEnvSetsMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MINIMAX_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfg := &Config{Providers: Presets()}
	s := Secrets{APIKeys: map[string]string{
		"minimax":   "mx",
		"anthropic": "an",
	}}
	s.ApplyEnv(cfg)
	if got := os.Getenv("MINIMAX_API_KEY"); got != "mx" {
		t.Errorf("MINIMAX_API_KEY: %q", got)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "an" {
		t.Errorf("ANTHROPIC_API_KEY: %q", got)
	}
}

func TestApplyEnvRespectsExisting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MINIMAX_API_KEY", "preset")

	cfg := &Config{Providers: Presets()}
	s := Secrets{APIKeys: map[string]string{"minimax": "new"}}
	s.ApplyEnv(cfg)
	if got := os.Getenv("MINIMAX_API_KEY"); got != "preset" {
		t.Errorf("existing env overwritten: %q", got)
	}
}

func TestApplyEnvMissingEntryIsNoop(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GHOST_API_KEY", "")
	cfg := &Config{Providers: Presets()}
	s := Secrets{APIKeys: map[string]string{"ghost": "value"}}
	// Does not panic and does not set anything.
	s.ApplyEnv(cfg)
	if got := os.Getenv("GHOST_API_KEY"); got != "" {
		t.Errorf("unexpected env set: %q", got)
	}
}

func TestApplyEnvNilCfg(t *testing.T) {
	s := Secrets{APIKeys: map[string]string{"x": "y"}}
	s.ApplyEnv(nil) // must not panic
}
