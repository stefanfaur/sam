package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func TestRenderThinkingCard_FullLive(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &thinkingCardState{Text: "Reading the plan", StartedAt: time.Now()}
	out := renderThinkingCard(th, tc, time.Now(), 0, "full", 80)
	if !strings.ContainsAny(out, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Errorf("missing spinner: %q", out)
	}
	if !strings.Contains(out, "Reading the plan") {
		t.Errorf("missing body: %q", out)
	}
}

func TestRenderThinkingCard_HeaderLive(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &thinkingCardState{Text: strings.Repeat("x", 250), StartedAt: time.Now()}
	out := renderThinkingCard(th, tc, time.Now(), 0, "header", 80)
	if !strings.Contains(out, "250 chars") {
		t.Errorf("missing char count: %q", out)
	}
	if strings.Contains(out, "xxxxx") {
		t.Errorf("body should be hidden in header mode: %q", out)
	}
}

func TestRenderThinkingCard_LongLineWraps(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	// Single natural-language line much longer than inner width. Include
	// spaces so word-wrap has break points.
	long := strings.Repeat("word ", 120) // 600 chars, spaces every 5
	tc := &thinkingCardState{Text: strings.TrimSpace(long), StartedAt: time.Now()}
	const inner = 40
	out := renderThinkingCard(th, tc, time.Now(), 0, "full", inner)
	// Every visible line (ignoring ANSI) must fit within inner; we assert a
	// forgiving bound since lipgloss may add minor padding/border glyphs.
	for _, line := range strings.Split(out, "\n") {
		if w := lipglossWidth(line); w > inner+2 {
			t.Fatalf("line wider than inner+2 (%d > %d): %q", w, inner+2, line)
		}
	}
	// Card must span multiple body lines — proves wrap kicked in.
	if n := strings.Count(out, "\n"); n < 4 {
		t.Fatalf("expected multi-line wrap, got %d newlines in:\n%s", n, out)
	}
}

func TestRenderThinkingCard_Settled(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &thinkingCardState{
		Text:      "thought content",
		StartedAt: time.Now().Add(-4200 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     1,
	}
	full := renderThinkingCard(th, tc, time.Now(), 0, "full", 80)
	hdr := renderThinkingCard(th, tc, time.Now(), 0, "header", 80)
	for _, out := range []string{full, hdr} {
		if !strings.Contains(out, "✧") {
			t.Errorf("missing settled glyph: %q", out)
		}
		if !strings.Contains(out, "thought for 4.2s") {
			t.Errorf("missing elapsed: %q", out)
		}
		if !strings.Contains(out, "[1]") {
			t.Errorf("missing hint: %q", out)
		}
	}
}
