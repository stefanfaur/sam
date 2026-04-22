package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SAM_PROVIDER", "")
	t.Setenv("SAM_MODEL", "")

	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Provider != "minimax" {
		t.Fatalf("default provider: %s", cfg.Provider)
	}
	if cfg.MaxTokens != 4096 {
		t.Fatalf("max_tokens: %d", cfg.MaxTokens)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `provider = "anthropic"
model = "claude-sonnet-4-6"
max_tokens = 8192

[providers.anthropic]
base_url = "https://example.com"
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Provider != "anthropic" || cfg.Model != "claude-sonnet-4-6" || cfg.MaxTokens != 8192 {
		t.Fatalf("bad cfg: %+v", cfg)
	}
	if cfg.Providers.Anthropic.BaseURL != "https://example.com" {
		t.Fatalf("base_url: %s", cfg.Providers.Anthropic.BaseURL)
	}
}

func TestOverridesWin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SAM_PROVIDER", "anthropic")

	cfg, err := Load(Overrides{Provider: "minimax", Model: "MiniMax-M2.7"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Provider != "minimax" || cfg.Model != "MiniMax-M2.7" {
		t.Fatalf("override not applied: %+v", cfg)
	}
}

func TestInvalidProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if _, err := Load(Overrides{Provider: "nope"}); err == nil {
		t.Fatal("expected invalid provider error")
	}
}
