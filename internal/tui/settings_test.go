package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSettingsDefaults(t *testing.T) {
	s := DefaultSettings()
	if s.Statusbar.Layout != "two-line" {
		t.Errorf("default layout = %q, want two-line", s.Statusbar.Layout)
	}
	if !s.Statusbar.Enabled {
		t.Error("default statusbar should be enabled")
	}
	if s.Theme.GlamourStyle != "dark" {
		t.Errorf("default glamour style = %q, want dark", s.Theme.GlamourStyle)
	}
	if !s.Statusbar.Segments.State {
		t.Error("state segment should be enabled by default")
	}
}

func TestLoadSettingsMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := LoadSettings()
	if !reflect.DeepEqual(s, DefaultSettings()) {
		t.Errorf("missing file should return defaults, got %+v", s)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	s := DefaultSettings()
	s.Statusbar.Layout = "one-line"
	s.Theme.Accent = "#ff00ff"
	if err := SaveSettings(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(dir, "sam", "settings.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}

	got := LoadSettings()
	if got.Statusbar.Layout != "one-line" {
		t.Errorf("layout = %q, want one-line", got.Statusbar.Layout)
	}
	if got.Theme.Accent != "#ff00ff" {
		t.Errorf("accent = %q, want #ff00ff", got.Theme.Accent)
	}
}

func TestSparseTomlKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	samDir := filepath.Join(dir, "sam")
	_ = os.MkdirAll(samDir, 0o755)
	content := "[statusbar]\nlayout = \"one-line\"\n"
	if err := os.WriteFile(filepath.Join(samDir, "settings.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := LoadSettings()
	if got.Statusbar.Layout != "one-line" {
		t.Errorf("layout = %q, want one-line", got.Statusbar.Layout)
	}
	if got.Theme.GlamourStyle != "dark" {
		t.Errorf("glamour = %q, should remain default dark", got.Theme.GlamourStyle)
	}
	if !got.Statusbar.Enabled {
		t.Error("enabled should remain default true")
	}
}

func TestValidateNormalizesUnknownLayout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	samDir := filepath.Join(dir, "sam")
	_ = os.MkdirAll(samDir, 0o755)
	content := "[statusbar]\nlayout = \"banana\"\nposition = \"above\"\n"
	if err := os.WriteFile(filepath.Join(samDir, "settings.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings()
	if got.Statusbar.Layout != "two-line" {
		t.Errorf("invalid layout should fall back to two-line, got %q", got.Statusbar.Layout)
	}
	if got.Statusbar.Position != "below" {
		t.Errorf("invalid position should fall back to below, got %q", got.Statusbar.Position)
	}
}
