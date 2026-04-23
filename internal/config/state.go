package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// State holds runtime-selected preferences persisted across sessions. It
// lives separately from config.toml so user-authored comments and formatting
// in config.toml are never rewritten by the TUI.
type State struct {
	Provider string `toml:"provider,omitempty"`
	Model    string `toml:"model,omitempty"`
}

// StatePath returns the path to state.toml under $XDG_STATE_HOME or
// ~/.local/state/sam/.
func StatePath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "sam", "state.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "sam", "state.toml")
}

// LoadState reads state.toml. Missing or malformed file returns a zero State.
func LoadState() State {
	var s State
	path := StatePath()
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = toml.Unmarshal(b, &s)
	return s
}

// SaveState writes state.toml atomically (tmp + rename).
func SaveState(s State) error {
	path := StatePath()
	if path == "" {
		return fmt.Errorf("cannot resolve state path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.toml")
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(s); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
