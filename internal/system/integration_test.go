package system_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/system"
)

// resolveForTest mirrors cmd/sam/main.go::resolveSystemPrompt without the
// logger. Keep the two in lockstep — any change to main's resolver must land
// here too, or these tests silently diverge from production.
func resolveForTest(cfg *config.Config, sysDir, model string) string {
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
		return base
	}
	var add string
	if system.FamilyPromptExists(sysDir, family) {
		add, _ = system.LoadFamilyPrompt(sysDir, family)
	} else {
		add = system.EmbeddedFamilyPrompt(family)
	}
	if add == "" {
		return base
	}
	return strings.TrimRight(base, "\n\t ") + "\n\n" + strings.TrimRight(add, "\n\t ")
}

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

func TestResolveSystemPrompt_OverrideTotal(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "override.md")
	if err := os.WriteFile(override, []byte("OVERRIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SystemPromptFile: override,
		PromptFamilies:   config.DefaultPromptFamilies(),
	}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if got != "OVERRIDE" {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestResolveSystemPrompt_BaseOnly_UnknownModel(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "random-model-x")
	if got != system.EmbeddedPrompt() {
		t.Fatalf("expected embedded base only, got %q", got)
	}
}

func TestResolveSystemPrompt_BasePlusFamily_Embedded(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	want := strings.TrimRight(system.EmbeddedPrompt(), "\n\t ") + "\n\n" + strings.TrimRight(system.EmbeddedFamilyPrompt("claude"), "\n\t ")
	if got != want {
		t.Fatalf("composition mismatch")
	}
}

func TestResolveSystemPrompt_DiskFamilyOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte("DISK FAMILY"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if !strings.HasSuffix(got, "\n\nDISK FAMILY") {
		t.Fatalf("disk family not applied: %q", got)
	}
}

func TestResolveSystemPrompt_EmptyFamilyFileMeansNoAppend(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if got != system.EmbeddedPrompt() {
		t.Fatalf("expected base only, got %q", got)
	}
	if strings.Contains(got, "use_parallel_tool_calls") {
		t.Fatal("embedded family leaked through empty disk file")
	}
}

func TestResolveSystemPrompt_MissingFamilyFileFallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if !strings.Contains(got, "use_parallel_tool_calls") {
		t.Fatalf("embedded family should apply when disk file missing: %q", got)
	}
}

// TestAllFamilyAddendaResolve — every family declared in
// DefaultPromptFamilies() must resolve a non-empty addendum from embedded
// defaults. Guards both the family map and the defaults/prompts/ dir
// against drift.
func TestAllFamilyAddendaResolve(t *testing.T) {
	for name := range config.DefaultPromptFamilies() {
		t.Run(name, func(t *testing.T) {
			got := system.EmbeddedFamilyPrompt(name)
			if strings.TrimSpace(got) == "" {
				t.Errorf("family %q resolved empty addendum from embedded defaults", name)
			}
		})
	}
}

// TestBasePrompt_SizeBudget guards against unintended base-prompt growth.
// Budget set to 1280 after SAFETY block addition (2026-04-24 redesign).
// Further growth requires explicit budget review — bump in same PR.
func TestBasePrompt_SizeBudget(t *testing.T) {
	const budget = 1280
	base := system.EmbeddedPrompt()
	if got := len(base); got >= budget {
		t.Errorf("base prompt size %d bytes >= %d budget; trim or raise budget", got, budget)
	}
}

// TestBasePrompt_ContainsSafetyBlock — guard SAFETY block presence so it
// cannot be silently removed.
func TestBasePrompt_ContainsSafetyBlock(t *testing.T) {
	base := system.EmbeddedPrompt()
	if !strings.Contains(base, "SAFETY.") {
		t.Errorf("base prompt missing SAFETY block header; got:\n%s", base)
	}
	if !strings.Contains(base, "user confirmation") {
		t.Errorf("base prompt SAFETY block missing confirmation language; got:\n%s", base)
	}
}

func TestResolveSystemPrompt_JoinFormatExactlyOneBlankLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "system-prompt.md"), []byte("BASE\n\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte("FAM\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	want := "BASE\n\nFAM"
	if got != want {
		t.Fatalf("join format wrong: got %q want %q", got, want)
	}
}
