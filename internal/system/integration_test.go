package system_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/system"
)

// resolvePrompt mirrors the lookup chain in cmd/sam/main.go: disk beats
// embedded, and cfg.LoadSystemPrompt layers CLI flag / config_file on top.
func resolvePrompt(cfg *config.Config, sysDir string) string {
	disk, _ := system.LoadSystemPrompt(sysDir)
	if disk == "" {
		disk = system.EmbeddedPrompt()
	}
	return cfg.LoadSystemPrompt(disk)
}

func TestPrecedence_EmbeddedOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SAM_HOME", dir) // directory exists but Seed never called.

	cfg := &config.Config{}
	got := resolvePrompt(cfg, system.DefaultDir())
	if got != system.EmbeddedPrompt() {
		t.Fatalf("expected embedded prompt, got %q", got)
	}
}

func TestPrecedence_DiskBeatsEmbedded(t *testing.T) {
	dir := t.TempDir()
	if err := system.Seed(dir, nil); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	custom := "disk override prompt"
	if err := os.WriteFile(filepath.Join(dir, "system-prompt.md"), []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	got := resolvePrompt(cfg, dir)
	if got != custom {
		t.Fatalf("got %q want %q", got, custom)
	}
}

func TestPrecedence_ConfigBeatsDisk(t *testing.T) {
	dir := t.TempDir()
	if err := system.Seed(dir, nil); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	// Disk differs from config-file content; config wins.
	if err := os.WriteFile(filepath.Join(dir, "system-prompt.md"), []byte("disk"), 0644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "prompt.md")
	want := "config file prompt"
	if err := os.WriteFile(cfgPath, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{SystemPromptFile: cfgPath}
	got := resolvePrompt(cfg, dir)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPrecedence_FlagBeatsConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "sam"), 0755); err != nil {
		t.Fatal(err)
	}
	baseFile := filepath.Join(t.TempDir(), "base.md")
	if err := os.WriteFile(baseFile, []byte("from config file"), 0644); err != nil {
		t.Fatal(err)
	}
	body := "provider = \"minimax\"\nsystem_prompt_file = \"" + baseFile + "\"\n"
	if err := os.WriteFile(filepath.Join(xdg, "sam", "config.toml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	flagFile := filepath.Join(t.TempDir(), "flag.md")
	want := "from CLI flag"
	if err := os.WriteFile(flagFile, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.Overrides{SystemPromptFile: flagFile})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.SystemPromptFile != flagFile {
		t.Fatalf("override not applied: %q", cfg.SystemPromptFile)
	}
	got := cfg.LoadSystemPrompt("fallback")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
