package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Settings struct {
	Statusbar StatusbarSettings `toml:"statusbar"`
	Theme     ThemeSettings     `toml:"theme"`
}

type StatusbarSettings struct {
	Enabled  bool              `toml:"enabled"`
	Position string            `toml:"position"`
	Layout   string            `toml:"layout"`
	Spinner  bool              `toml:"spinner"`
	Colors   bool              `toml:"colors"`
	Elapsed  bool              `toml:"elapsed"`
	Segments StatusbarSegments `toml:"segments"`
}

type StatusbarSegments struct {
	State      bool `toml:"state"`
	Model      bool `toml:"model"`
	Provider   bool `toml:"provider"`
	Cwd        bool `toml:"cwd"`
	Git        bool `toml:"git"`
	Iterations bool `toml:"iterations"`
	Context    bool `toml:"context"`
	Tokens     bool `toml:"tokens"`
	Keybinds   bool `toml:"keybinds"`
}

type ThemeSettings struct {
	GlamourStyle    string `toml:"glamour_style"`
	Accent          string `toml:"accent"`
	Muted           string `toml:"muted"`
	UserBorder      string `toml:"user_border"`
	AssistantFg     string `toml:"assistant_fg"`
	ErrorFg         string `toml:"error_fg"`
	StateThinking   string `toml:"state_thinking"`
	StateResponding string `toml:"state_responding"`
	StateTool       string `toml:"state_tool"`
	StateError      string `toml:"state_error"`
	StateApproval   string `toml:"state_approval"`
}

func DefaultSettings() Settings {
	return Settings{
		Statusbar: StatusbarSettings{
			Enabled:  true,
			Position: "below",
			Layout:   "two-line",
			Spinner:  true,
			Colors:   true,
			Elapsed:  true,
			Segments: StatusbarSegments{
				State: true, Model: true, Provider: true,
				Cwd: true, Git: true, Iterations: true,
				Context: true, Tokens: true,
			},
		},
		Theme: ThemeSettings{
			GlamourStyle:    "dark",
			Accent:          "#6366f1",
			Muted:           "#737373",
			UserBorder:      "#8b5cf6",
			AssistantFg:     "",
			ErrorFg:         "#ef4444",
			StateThinking:   "#60a5fa",
			StateResponding: "#34d399",
			StateTool:       "#fbbf24",
			StateError:      "#f87171",
			StateApproval:   "#a78bfa",
		},
	}
}

func settingsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sam", "settings.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sam", "settings.toml")
}

func LoadSettings() Settings {
	s := DefaultSettings()
	path := settingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = toml.Unmarshal(data, &s)
	validate(&s)
	return s
}

func SaveSettings(s Settings) error {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(s)
}

func validate(s *Settings) {
	if s.Statusbar.Layout != "one-line" && s.Statusbar.Layout != "two-line" {
		s.Statusbar.Layout = "two-line"
	}
	if s.Statusbar.Position != "below" {
		s.Statusbar.Position = "below"
	}
}
