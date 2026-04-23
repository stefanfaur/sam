package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func readManifest(t *testing.T, dir string) manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return m
}

func TestSeed_FreshDir(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, nil); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	expected := []string{
		"system-prompt.md",
		"tools/read.md",
		"tools/write.md",
		"tools/edit.md",
		"tools/bash.md",
	}
	m := readManifest(t, dir)
	for _, rel := range expected {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if sha256Hex(b) != m.Hashes[rel] {
			t.Fatalf("manifest hash mismatch for %s", rel)
		}
	}
}

func TestSeed_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, nil); err != nil {
		t.Fatalf("Seed 1: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "system-prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Seed(dir, nil); err != nil {
		t.Fatalf("Seed 2: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "system-prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("content changed between idempotent seeds")
	}
}

func TestSeed_UserEditedSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, nil); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	custom := []byte("user override content")
	promptPath := filepath.Join(dir, "system-prompt.md")
	if err := os.WriteFile(promptPath, custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(dir, nil); err != nil {
		t.Fatalf("re-Seed: %v", err)
	}
	got, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("user edit clobbered: got %q", got)
	}
}

// TestSeed_UnchangedOverwritten simulates the "embedded changed" case by
// writing a fake file and a manifest whose hash matches the fake file.
// Re-seed must overwrite the fake with the real embedded content.
func TestSeed_UnchangedOverwritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	fake := []byte("old embedded content")
	promptPath := filepath.Join(dir, "system-prompt.md")
	if err := os.WriteFile(promptPath, fake, 0644); err != nil {
		t.Fatal(err)
	}
	m := newManifest()
	m.Hashes["system-prompt.md"] = sha256Hex(fake)
	if err := saveManifest(dir, m); err != nil {
		t.Fatal(err)
	}

	if err := Seed(dir, nil); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	got, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == string(fake) {
		t.Fatal("fake content not overwritten")
	}
	if string(got) != EmbeddedPrompt() {
		t.Fatal("overwrite did not use embedded content")
	}
	m2 := readManifest(t, dir)
	if m2.Hashes["system-prompt.md"] != sha256Hex([]byte(EmbeddedPrompt())) {
		t.Fatal("manifest not updated to embedded hash")
	}
}

func TestSeed_MissingManifest(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, nil); err != nil {
		t.Fatal(err)
	}
	custom := []byte("user content")
	promptPath := filepath.Join(dir, "system-prompt.md")
	if err := os.WriteFile(promptPath, custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}

	if err := Seed(dir, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("user content lost after manifest rebuild: got %q", got)
	}
	m := readManifest(t, dir)
	if m.Hashes["system-prompt.md"] != sha256Hex(custom) {
		t.Fatal("rebuilt manifest hash mismatch")
	}
}

func TestSeed_CorruptManifest(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, nil); err != nil {
		t.Fatal(err)
	}
	custom := []byte("user content")
	promptPath := filepath.Join(dir, "system-prompt.md")
	if err := os.WriteFile(promptPath, custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Seed(dir, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("user content lost after corrupt manifest: got %q", got)
	}
}

func TestSeed_Concurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Seed(dir, nil); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent Seed: %v", err)
	}

	m := readManifest(t, dir)
	b, err := os.ReadFile(filepath.Join(dir, "system-prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(b) != m.Hashes["system-prompt.md"] {
		t.Fatal("manifest hash diverged from disk after concurrent seed")
	}
	if string(b) != EmbeddedPrompt() {
		t.Fatal("concurrent seed did not yield embedded content")
	}
}

func TestLoadSystemPrompt_MissingAndPresent(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadSystemPrompt(dir)
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if s != "" {
		t.Fatalf("missing want empty, got %q", s)
	}
	want := "hello prompt"
	if err := os.WriteFile(filepath.Join(dir, "system-prompt.md"), []byte(want), 0644); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSystemPrompt(dir)
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	if s != want {
		t.Fatalf("present: got %q want %q", s, want)
	}
}

func TestLoadToolDescription_MissingAndPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := LoadToolDescription(dir, "Read")
	if err != nil || s != "" {
		t.Fatalf("missing: s=%q err=%v", s, err)
	}
	want := "custom read"
	if err := os.WriteFile(filepath.Join(dir, "tools", "read.md"), []byte(want+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err = LoadToolDescription(dir, "Read")
	if err != nil {
		t.Fatal(err)
	}
	if s != want {
		t.Fatalf("got %q want %q", s, want)
	}
}
