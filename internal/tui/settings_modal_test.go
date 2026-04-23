package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/skills"
)

func newApplyRoot(t *testing.T) *Model {
	t.Helper()
	s := DefaultSettings()
	return &Model{
		settings: s,
		theme:    NewTheme(s.Theme),
		status: statusbarModel{
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			state:    "idle",
			maxIter:  25,
		},
	}
}

func TestSettingsModalApplyCancelled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := newApplyRoot(t)
	original := root.settings
	mm := newSettingsModal(root.settings, "anthropic", nil, nil)
	mm.cancelled = true
	cmd := mm.Apply(root)
	if cmd == nil {
		t.Error("expected an info command")
	}
	if root.settings.Statusbar.Layout != original.Statusbar.Layout {
		t.Error("settings should not change on cancel")
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sam", "settings.toml")
	if _, err := os.Stat(path); err == nil {
		t.Error("settings file should not be written on cancel")
	}
}

func TestSettingsModalApplyPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	root := newApplyRoot(t)
	mm := newSettingsModal(root.settings, "anthropic", nil, nil)
	mm.pending.Statusbar.Layout = "one-line"
	mm.pending.Theme.Accent = "#abcdef"
	cmd := mm.Apply(root)
	if cmd == nil {
		t.Error("expected info command")
	}
	if root.settings.Statusbar.Layout != "one-line" {
		t.Errorf("root.settings.Layout = %q, want one-line", root.settings.Statusbar.Layout)
	}
	if root.theme == nil {
		t.Fatal("theme should be replaced")
	}
	path := filepath.Join(dir, "sam", "settings.toml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("settings file not written: %v", err)
	}
}

func TestSettingsModalTabNavReachesSkills(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	mkSkill(t, root, "sample")
	reg := skills.NewRegistry(
		[]skills.RootSpec{{Path: root, Label: "personal", Source: skills.SourcePersonal}},
		skills.Overrides{}, skills.TrustList{}, BuiltinNames, nil,
	)
	reg.Load()
	mm := newSettingsModal(DefaultSettings(), "anthropic", nil, reg)
	if mm.active != tabStatusline {
		t.Fatalf("initial tab = %v, want tabStatusline", mm.active)
	}
	// Tab tabStatusline → tabProviders → tabTheme → tabSkills.
	for i := 0; i < 3; i++ {
		mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if mm.active != tabSkills {
		t.Fatalf("after 3x Tab, active = %v, want tabSkills", mm.active)
	}
	view := mm.View()
	if !strings.Contains(view, "sample") {
		t.Errorf("Skills tab view missing fixture skill: %s", view)
	}
	// Shift+Tab returns to tabTheme.
	mm.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if mm.active != tabTheme {
		t.Errorf("after Shift+Tab from Skills, active = %v", mm.active)
	}
}

func TestSettingsModalApplyFallsBackOnBadGlamour(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := newApplyRoot(t)
	mm := newSettingsModal(root.settings, "anthropic", nil, nil)
	// Unknown glamour style — NewTheme falls back to renderer-less glam if the
	// SDK rejects the style. Apply's guard only kicks in when Glamour() is nil.
	mm.pending.Theme.GlamourStyle = "nonexistent-style-xyz"
	cmd := mm.Apply(root)
	if cmd == nil {
		t.Error("expected info command")
	}
}
