package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedFamilyPrompt_BundledFamilies(t *testing.T) {
	for _, family := range []string{"claude", "minimax", "kimi-k2", "trinity", "gpt", "deepseek"} {
		t.Run(family, func(t *testing.T) {
			got := EmbeddedFamilyPrompt(family)
			if got == "" {
				t.Fatalf("embedded %s.md empty or missing", family)
			}
		})
	}
}

func TestEmbeddedFamilyPrompt_Unknown(t *testing.T) {
	if got := EmbeddedFamilyPrompt("does-not-exist"); got != "" {
		t.Fatalf("unknown family returned content: %q", got)
	}
}

func TestLoadFamilyPrompt_MissingAndPresent(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadFamilyPrompt(dir, "claude")
	if err != nil {
		t.Fatalf("unexpected err on missing: %v", err)
	}
	if got != "" {
		t.Fatalf("missing should be empty: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte("hello family   \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = LoadFamilyPrompt(dir, "claude")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "hello family" {
		t.Fatalf("content: %q", got)
	}
}

func TestFamilyPromptExists(t *testing.T) {
	dir := t.TempDir()
	if FamilyPromptExists(dir, "claude") {
		t.Fatal("missing file reported as existing")
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !FamilyPromptExists(dir, "claude") {
		t.Fatal("empty-but-present file reported as missing")
	}
}
