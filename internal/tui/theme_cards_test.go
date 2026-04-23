package tui

import (
	"strings"
	"testing"
)

func TestTheme_ToolCardStylesPresent(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	s := th.ToolCard.Render("x")
	if !strings.Contains(s, "x") {
		t.Errorf("ToolCard didn't render body: %q", s)
	}
	if th.ToolCardHeader.GetForeground() == nil {
		t.Error("ToolCardHeader foreground unset")
	}
}

func TestTheme_ThinkingCardBodyHasBackground(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	bg := th.ThinkingCardBody.GetBackground()
	if bg == nil {
		t.Fatal("ThinkingCardBody background unset")
	}
}

func TestTheme_SubtleBgDependsOnDark(t *testing.T) {
	th := &Theme{dark: true}
	if got := th.subtleBg(); string(got) != "235" {
		t.Errorf("dark bg = %q, want 235", got)
	}
	th.dark = false
	if got := th.subtleBg(); string(got) != "254" {
		t.Errorf("light bg = %q, want 254", got)
	}
}
