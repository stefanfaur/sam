package tui

import "time"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerTickInterval = 80 * time.Millisecond

type spinnerState struct {
	frame int
	on    bool
}

func (s *spinnerState) advance() { s.frame = (s.frame + 1) % len(spinnerFrames) }

func (s *spinnerState) glyph() string {
	if !s.on {
		return ""
	}
	return spinnerFrames[s.frame]
}
