package tui

import (
	"testing"
	"time"
)

func newTestModel() *Model {
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

func TestHumanK(t *testing.T) {
	cases := map[int]string{
		0:         "0",
		999:       "999",
		1_500:     "1.5k",
		12_345:    "12.3k",
		2_500_000: "2.5M",
	}
	for n, want := range cases {
		if got := humanK(n); got != want {
			t.Errorf("humanK(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSegStateIdleIsVisible(t *testing.T) {
	m := newTestModel()
	s := segState(m)
	if !s.visible {
		t.Error("idle state should be visible")
	}
}

func TestSegStateDisabled(t *testing.T) {
	m := newTestModel()
	m.settings.Statusbar.Segments.State = false
	if s := segState(m); s.visible {
		t.Error("disabled state should be invisible")
	}
}

func TestSegElapsedHiddenWhenNoTurn(t *testing.T) {
	m := newTestModel()
	if s := segElapsed(m); s.visible {
		t.Error("elapsed should hide without pending turn")
	}
}

func TestSegElapsedVisibleDuringTurn(t *testing.T) {
	m := newTestModel()
	m.pending = &pendingTurn{}
	m.turnStart = time.Now().Add(-1500 * time.Millisecond)
	s := segElapsed(m)
	if !s.visible {
		t.Error("elapsed should show during turn")
	}
	if s.text == "" {
		t.Error("elapsed text should be non-empty")
	}
}

func TestSegGitEmpty(t *testing.T) {
	m := newTestModel()
	if s := segGit(m); s.visible {
		t.Error("git should hide when branch empty")
	}
}

func TestSegGitBranchDirty(t *testing.T) {
	m := newTestModel()
	m.git = gitInfo{branch: "main", dirty: true}
	s := segGit(m)
	if !s.visible {
		t.Error("git segment should be visible")
	}
	if !contains(s.text, "main") || !contains(s.text, "●") {
		t.Errorf("git text missing branch/dirty marker: %q", s.text)
	}
}

func TestSegCtxWithoutUsage(t *testing.T) {
	m := newTestModel()
	s := segCtx(m, 200_000)
	if !s.visible || !contains(s.text, "—") {
		t.Errorf("ctx with no usage should show —, got %q", s.text)
	}
}

func TestSegCtxWithUsage(t *testing.T) {
	m := newTestModel()
	m.status.lastIterIn = 40_000
	s := segCtx(m, 200_000)
	if !s.visible || !contains(s.text, "20%") {
		t.Errorf("ctx 40k/200k should show 20%%, got %q", s.text)
	}
}

func TestSegTokensHiddenAtZero(t *testing.T) {
	m := newTestModel()
	if s := segTokens(m); s.visible {
		t.Error("tokens should hide at 0")
	}
}

func TestSegTokensVisibleWithUsage(t *testing.T) {
	m := newTestModel()
	m.status.turnIn = 12_345
	m.status.turnOut = 678
	s := segTokens(m)
	if !s.visible {
		t.Error("tokens should show with usage")
	}
	if !contains(s.text, "12.3k") || !contains(s.text, "678") {
		t.Errorf("tokens text missing values: %q", s.text)
	}
}

func TestSegTokensShowsEffectiveAndCached(t *testing.T) {
	m := newTestModel()
	m.status.turnIn = 1_400_000
	m.status.turnCacheRead = 1_380_000
	m.status.turnOut = 1_100
	s := segTokens(m)
	if !s.visible {
		t.Fatal("tokens should show")
	}
	// effective = 20k; cached = 1.4M
	if !contains(s.text, "20.0k") {
		t.Errorf("expected effective 20.0k in text: %q", s.text)
	}
	if !contains(s.text, "1.4M") || !contains(s.text, "⚡") {
		t.Errorf("expected cached 1.4M⚡ in text: %q", s.text)
	}
	if !contains(s.text, "1.1k") {
		t.Errorf("expected output 1.1k in text: %q", s.text)
	}
}

func TestSegTokensNoCacheOmitsBadge(t *testing.T) {
	m := newTestModel()
	m.status.turnIn = 5_000
	m.status.turnOut = 200
	s := segTokens(m)
	if contains(s.text, "⚡") {
		t.Errorf("cache badge should hide without cache: %q", s.text)
	}
	if !contains(s.text, "5.0k") {
		t.Errorf("expected full in: %q", s.text)
	}
}

func TestSegIterDisabled(t *testing.T) {
	m := newTestModel()
	m.settings.Statusbar.Segments.Iterations = false
	if s := segIter(m); s.visible {
		t.Error("iter should hide when disabled")
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && (h == n || indexOf(h, n) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
