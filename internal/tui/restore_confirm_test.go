package tui

import (
	"strings"
	"testing"
)

func TestRestoreConfirm_RequiresModeBeforeConfirm(t *testing.T) {
	r := newRestoreConfirm(2, []string{"a.go"}, nil)
	if r.Confirm() {
		t.Fatal("Confirm should fail with unchosen mode")
	}
	if r.IsConfirmed() {
		t.Fatal("not confirmed yet")
	}
	r.SetMode(restoreModeConvOnly)
	if !r.Confirm() {
		t.Fatal("Confirm should succeed once mode is set")
	}
	if !r.IsConfirmed() {
		t.Fatal("should be confirmed")
	}
}

func TestRestoreConfirm_ConvOnlyReturnsNoPaths(t *testing.T) {
	r := newRestoreConfirm(1, []string{"a.go", "b.go"}, nil)
	r.SetMode(restoreModeConvOnly)
	if got := r.PathsToRestore(); got != nil {
		t.Fatalf("conv-only should restore no files, got %v", got)
	}
}

func TestRestoreConfirm_ExcludesOverlapsByDefault(t *testing.T) {
	r := newRestoreConfirm(1, []string{"a.go", "b.go", "c.go"}, []string{"b.go"})
	r.SetMode(restoreModeConvAndCode)
	got := r.PathsToRestore()
	if len(got) != 2 || got[0] != "a.go" || got[1] != "c.go" {
		t.Fatalf("got %v, want [a.go c.go]", got)
	}
}

func TestRestoreConfirm_IncludesOverlapsWhenToggled(t *testing.T) {
	r := newRestoreConfirm(1, []string{"a.go", "b.go"}, []string{"b.go"})
	r.SetMode(restoreModeConvAndCode)
	r.ToggleIncludeOverlaps()
	got := r.PathsToRestore()
	if len(got) != 2 {
		t.Fatalf("toggle on: got %v", got)
	}
	r.ToggleIncludeOverlaps()
	if len(r.PathsToRestore()) != 1 {
		t.Fatalf("toggle off: got %v", r.PathsToRestore())
	}
}

func TestRestoreConfirm_RenderShowsOverlapMarker(t *testing.T) {
	r := newRestoreConfirm(1, []string{"a.go", "b.go"}, []string{"b.go"})
	r.SetMode(restoreModeConvAndCode)
	out := r.Render(NewTheme(ThemeSettings{}), 80)
	if !strings.Contains(out, "modified by you") {
		t.Fatalf("missing overlap marker:\n%s", out)
	}
	if !strings.Contains(out, "Rewind to turn 1") {
		t.Fatalf("missing header:\n%s", out)
	}
}

func TestRestoreConfirm_RenderTruncatesLongList(t *testing.T) {
	paths := make([]string, 25)
	for i := range paths {
		paths[i] = "f" + string(rune('a'+i)) + ".go"
	}
	r := newRestoreConfirm(1, paths, nil)
	r.SetMode(restoreModeConvAndCode)
	out := r.Render(NewTheme(ThemeSettings{}), 80)
	if !strings.Contains(out, "more") {
		t.Fatalf("missing truncation marker:\n%s", out)
	}
}
