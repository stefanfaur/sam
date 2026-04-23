package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
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
	for _, name := range []string{"minimax", "anthropic", "openai", "arcee", "moonshot"} {
		if _, ok := cfg.Providers[name]; !ok {
			t.Errorf("preset %q missing", name)
		}
	}
	moon := cfg.Providers["moonshot"]
	if moon.Wire != "openai" || moon.BaseURL != "https://api.moonshot.ai/v1" ||
		moon.APIKeyEnv != "KIMI_API_KEY" || moon.DefaultModel != "kimi-k2.6" {
		t.Errorf("moonshot preset shape: %+v", moon)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `provider = "anthropic"
model = "claude-sonnet-4-6"
max_tokens = 8192

[providers.anthropic]
wire = "anthropic"
base_url = "https://example.com"
api_key_env = "ANTHROPIC_API_KEY"
default_model = "claude-sonnet-4-6"
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
	if cfg.Providers["anthropic"].BaseURL != "https://example.com" {
		t.Fatalf("base_url: %s", cfg.Providers["anthropic"].BaseURL)
	}
}

func TestOverridesWin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
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
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	if _, err := Load(Overrides{Provider: "nope"}); err == nil {
		t.Fatal("expected invalid provider error")
	}
}

func TestFullReplaceOnUserEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	// User redeclares openai with only base_url + wire — default_model
	// must NOT survive from the preset.
	body := `provider = "openai"

[providers.openai]
wire = "openai"
base_url = "http://localhost:1234/v1"
api_key_env = "OPENAI_API_KEY"
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Providers["openai"].DefaultModel; got != "" {
		t.Fatalf("expected full-replace to clear default_model, got %q", got)
	}
	if got := cfg.Providers["openai"].BaseURL; got != "http://localhost:1234/v1" {
		t.Fatalf("base_url: %q", got)
	}
}

func TestUserDefinedProviderAppends(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `provider = "groq"

[providers.groq]
wire = "openai"
base_url = "https://api.groq.com/openai/v1"
api_key_env = "GROQ_API_KEY"
default_model = "llama-3.3-70b-versatile"
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.Providers["groq"]; !ok {
		t.Fatal("groq entry missing")
	}
	if cfg.Providers["groq"].Name != "groq" {
		t.Fatalf("name not backfilled: %q", cfg.Providers["groq"].Name)
	}
	// Presets still present alongside the user entry.
	if _, ok := cfg.Providers["anthropic"]; !ok {
		t.Fatal("preset anthropic disappeared")
	}
}

func TestInvalidWireErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `provider = "bogus"

[providers.bogus]
wire = "grpc"
base_url = ""
api_key_env = "X"
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Overrides{}); err == nil {
		t.Fatal("expected wire validation error")
	}
}

func TestPerModelReasoningEffort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `provider = "openai"

[models."gpt-5"]
reasoning_effort = "high"
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Models["gpt-5"].ReasoningEffort; got != "high" {
		t.Fatalf("reasoning_effort: %q", got)
	}
}

func TestDefaultReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"kimi-k2.6":    "high",
		"kimi-k2.5":    "high",
		"gpt-4o":       "",
		"gpt-5":        "",
		"MiniMax-M2.7": "",
		"":             "",
	}
	for model, want := range cases {
		if got := DefaultReasoningEffort(model); got != want {
			t.Errorf("DefaultReasoningEffort(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestModelThinkingBudgetDefaults(t *testing.T) {
	cfg := &Config{}
	cases := map[string]int{
		"MiniMax-M2.7":             32_000,
		"MiniMax-M1":               32_000,
		"claude-sonnet-4-5":        0,
		"trinity-large-thinking":   0,
		"gpt-4o":                   0,
	}
	for model, want := range cases {
		if got := cfg.ModelThinkingBudget(model); got != want {
			t.Errorf("%s: got %d want %d", model, got, want)
		}
	}
}

func TestModelThinkingBudgetExplicitOverride(t *testing.T) {
	zero := 0
	eightK := 8_000
	cfg := &Config{Models: map[string]ModelConfig{
		"MiniMax-M2.7": {ThinkingBudgetTokens: &zero},
		"claude-sonnet-4-5": {ThinkingBudgetTokens: &eightK},
	}}
	if got := cfg.ModelThinkingBudget("MiniMax-M2.7"); got != 0 {
		t.Errorf("explicit 0 should disable: got %d", got)
	}
	if got := cfg.ModelThinkingBudget("claude-sonnet-4-5"); got != 8_000 {
		t.Errorf("explicit 8k: got %d", got)
	}
}

func TestContextWindowPrefixRules(t *testing.T) {
	cfg := &Config{}
	cases := map[string]int{
		"gpt-5":                  400_000,
		"o1-preview":             400_000,
		"o3-mini":                400_000,
		"o4-mini":                400_000,
		"gpt-4o":                 128_000,
		"gpt-4.1-mini":           128_000,
		"deepseek-r1":            131_072,
		"deepseek-v3":            131_072,
		"trinity-large-thinking": 512_000,
		"claude-sonnet-4-5":      200_000,
		"MiniMax-M2.7":           1_000_000,
	}
	for name, want := range cases {
		if got := cfg.ModelContextWindow(name); got != want {
			t.Errorf("%s: got %d want %d", name, got, want)
		}
	}
}
