package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPromptFamilies_Shape(t *testing.T) {
	fams := DefaultPromptFamilies()
	want := map[string][]string{
		"claude":   {"claude-opus", "claude-sonnet", "claude-haiku"},
		"minimax":  {"MiniMax-"},
		"kimi-k2":  {"kimi-k2"},
		"trinity":  {"trinity-"},
		"gpt":      {"gpt-5", "gpt-4o", "gpt-4.1", "o1", "o3", "o4"},
		"deepseek": {"deepseek-"},
	}
	for name, prefixes := range want {
		got, ok := fams[name]
		if !ok {
			t.Errorf("family %q missing", name)
			continue
		}
		if len(got.Prefixes) != len(prefixes) {
			t.Errorf("family %q: prefix count %d want %d", name, len(got.Prefixes), len(prefixes))
			continue
		}
		for i, p := range prefixes {
			if got.Prefixes[i] != p {
				t.Errorf("family %q prefix[%d]: %q want %q", name, i, got.Prefixes[i], p)
			}
		}
	}
}

// cleanConfigEnv zeros every env var that can override config defaults so a
// developer's shell env (SAM_PROVIDER=foo) does not leak into Load.
func cleanConfigEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("SAM_PROVIDER", "")
	t.Setenv("SAM_MODEL", "")
}

func TestLoadSeedsDefaultPromptFamilies(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, name := range []string{"claude", "minimax", "kimi-k2", "trinity", "gpt", "deepseek"} {
		if _, ok := cfg.PromptFamilies[name]; !ok {
			t.Errorf("default family %q missing after Load", name)
		}
	}
}

func TestPromptFamilies_ConfigFullReplace(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[prompt_families.claude]
prefixes = ["claude-5", "claude-6"]
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.PromptFamilies["claude"].Prefixes
	if len(got) != 2 || got[0] != "claude-5" || got[1] != "claude-6" {
		t.Fatalf("full-replace failed: %v", got)
	}
}

func TestPromptFamilies_UserFamilyAddsToBundled(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[prompt_families.qwen]
prefixes = ["qwen-", "Qwen", "openrouter/qwen/"]
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.PromptFamilies["qwen"]; !ok {
		t.Fatal("qwen family missing")
	}
	if _, ok := cfg.PromptFamilies["claude"]; !ok {
		t.Fatal("bundled claude disappeared after user family added")
	}
}

func TestPromptFamilies_ConfigDisable(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[prompt_families.claude]
prefixes = []
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.PromptFamilies["claude"].Prefixes; len(got) != 0 {
		t.Fatalf("disable-by-empty failed: %v", got)
	}
}
