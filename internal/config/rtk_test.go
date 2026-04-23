package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateConfig points config lookup at a temp XDG dir and clears env/state
// that would perturb Load. Returns the config.toml path inside the tempdir.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Use the same tempdir for state to avoid LoadState pulling in user data.
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("SAM_PROVIDER", "")
	t.Setenv("SAM_MODEL", "")
	// HOME pointed at tempdir prevents fallback paths from reading user's home.
	t.Setenv("HOME", dir)

	cfgDir := filepath.Join(dir, "sam")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cfgDir, "config.toml")
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRTKDefaultAuto(t *testing.T) {
	isolateConfig(t)
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RTK.Mode != "auto" {
		t.Fatalf("default mode should be 'auto', got %q", cfg.RTK.Mode)
	}
}

func TestRTKFromTOML(t *testing.T) {
	path := isolateConfig(t)
	writeConfig(t, path, `
provider = "minimax"

[rtk]
mode = "on"
`)
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RTK.Mode != "on" {
		t.Fatalf("expected mode='on', got %q", cfg.RTK.Mode)
	}
}

func TestRTKInvalidClampsToAuto(t *testing.T) {
	path := isolateConfig(t)
	writeConfig(t, path, `
[rtk]
mode = "bogus"
`)
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RTK.Mode != "auto" {
		t.Fatalf("invalid mode must clamp to 'auto', got %q", cfg.RTK.Mode)
	}
}

func TestRTKModeOff(t *testing.T) {
	path := isolateConfig(t)
	writeConfig(t, path, `
[rtk]
mode = "off"
`)
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RTK.Mode != "off" {
		t.Fatalf("expected 'off', got %q", cfg.RTK.Mode)
	}
}
