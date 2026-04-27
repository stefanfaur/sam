package tui

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestFuzzyFilterEmptyQuery(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go"}
	got := fuzzyFilter("", files)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
}

func TestFuzzyFilterMatches(t *testing.T) {
	files := []string{
		"internal/tui/app.go",
		"internal/tui/update.go",
		"internal/agent/agent.go",
		"cmd/sam/main.go",
	}
	got := fuzzyFilter("app", files)
	if len(got) == 0 {
		t.Fatal("no matches for 'app'")
	}
	if got[0] != "internal/tui/app.go" {
		t.Errorf("top match: %q, want internal/tui/app.go", got[0])
	}
}

func TestFilePickerNavigation(t *testing.T) {
	fp := newFilePicker([]string{"a.go", "b.go", "c.go"})
	if fp.selected != 0 {
		t.Errorf("initial selected: %d", fp.selected)
	}
	fp.moveSelection(1)
	if fp.selected != 1 {
		t.Errorf("after +1: %d", fp.selected)
	}
	fp.moveSelection(5) // clamps
	if fp.selected != 2 {
		t.Errorf("after clamp: %d", fp.selected)
	}
	fp.moveSelection(-10)
	if fp.selected != 0 {
		t.Errorf("after -clamp: %d", fp.selected)
	}
}

func TestFilePickerSetQueryFiltersAndResetsSelection(t *testing.T) {
	fp := newFilePicker([]string{"alpha.go", "beta.go", "alphabet.go"})
	fp.moveSelection(1)
	fp.setQuery("alph")
	if fp.selected != 0 {
		t.Errorf("selected not reset to 0: %d", fp.selected)
	}
	if len(fp.filtered) != 2 {
		t.Errorf("filtered: %v", fp.filtered)
	}
}

func TestFilePickerCurrentEmpty(t *testing.T) {
	fp := newFilePicker(nil)
	if got := fp.current(); got != "" {
		t.Errorf("current on empty: %q", got)
	}
}

func TestGetFilesRecursiveSkipsHiddenAndBuildDirs(t *testing.T) {
	dir := t.TempDir()
	mustMkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	mustWrite := func(p string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	mustMkdir("src")
	mustMkdir(".git")
	mustMkdir("node_modules")
	mustMkdir("vendor/lib")
	mustWrite("src/main.go")
	mustWrite("README.md")
	mustWrite(".hidden.txt")
	mustWrite(".git/HEAD")
	mustWrite("node_modules/junk.js")
	mustWrite("vendor/lib/x.go")

	files, err := getFilesRecursive(dir)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(files)
	wantPresent := []string{"README.md", "src/main.go"}
	wantAbsent := []string{".hidden.txt", ".git/HEAD", "node_modules/junk.js", "vendor/lib/x.go"}
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	for _, w := range wantPresent {
		if !have[w] {
			t.Errorf("missing %q in walk; got %v", w, files)
		}
	}
	for _, w := range wantAbsent {
		if have[w] {
			t.Errorf("walk should skip %q; got %v", w, files)
		}
	}
}
