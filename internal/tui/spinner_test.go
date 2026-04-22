package tui

import "testing"

func TestSpinnerAdvanceWraps(t *testing.T) {
	s := spinnerState{on: true}
	for i := 0; i < len(spinnerFrames)*2+3; i++ {
		s.advance()
	}
	if s.frame < 0 || s.frame >= len(spinnerFrames) {
		t.Errorf("frame out of range: %d", s.frame)
	}
}

func TestSpinnerGlyphEmptyWhenOff(t *testing.T) {
	s := spinnerState{on: false}
	if g := s.glyph(); g != "" {
		t.Errorf("glyph off = %q, want empty", g)
	}
}

func TestSpinnerGlyphNonEmptyWhenOn(t *testing.T) {
	s := spinnerState{on: true}
	if g := s.glyph(); g == "" {
		t.Error("glyph on should be non-empty")
	}
}
