package tui

import (
	"strings"
	"testing"
	"time"
)

func TestModel_RecordInvokeBoundedRing(t *testing.T) {
	m := &Model{}
	for i := 0; i < maxRecentInvokes+5; i++ {
		m.recordInvoke(skillInvocation{Header: "/x", Body: "body", Source: "personal"})
	}
	if len(m.recentInvokes) != maxRecentInvokes {
		t.Errorf("len = %d, want %d", len(m.recentInvokes), maxRecentInvokes)
	}
}

func TestParseNonNegInt(t *testing.T) {
	cases := map[string]int{"0": 0, "7": 7, "42": 42}
	for in, want := range cases {
		got, err := parseNonNegInt(in)
		if err != nil || got != want {
			t.Errorf("parseNonNegInt(%q) = (%d, %v), want %d", in, got, err, want)
		}
	}
	if _, err := parseNonNegInt("x"); err == nil {
		t.Error("expected err for non-digit")
	}
	if _, err := parseNonNegInt("-1"); err == nil {
		t.Error("expected err for '-1'")
	}
}

func TestParseCommand_ShowSkill(t *testing.T) {
	cmd, arg, _ := parseCommand("/show-skill 3", nil)
	if cmd != CmdShowSkill {
		t.Errorf("cmd = %q", cmd)
	}
	if arg != "3" {
		t.Errorf("arg = %q", arg)
	}
	cmd, arg, _ = parseCommand("/show-skill", nil)
	if cmd != CmdShowSkill || arg != "" {
		t.Errorf("no-arg case: cmd=%q arg=%q", cmd, arg)
	}
}

func TestParseCommand_SkillCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	mkSkill(t, root, "review-pr")
	reg := newRegistryForTest(t, root)
	cmd, _, sk := parseCommand("/REVIEW-PR 1", reg)
	if cmd != CmdSkill {
		t.Errorf("uppercased cmd = %q", cmd)
	}
	if sk == nil || sk.Name != "review-pr" {
		t.Errorf("sk = %+v", sk)
	}
}

func TestRenderSkillCard_LiveAndStatic(t *testing.T) {
	theme := NewTheme(DefaultSettings().Theme)
	sc := &skillCardState{
		Header:    "/review-pr 123",
		Body:      strings.Repeat("x", 200),
		Source:    "personal",
		Index:     2,
		StartedAt: time.Now().Add(-1500 * time.Millisecond),
	}
	live := renderSkillCard(theme, sc, 1500*time.Millisecond, 3, true)
	static := renderSkillCard(theme, sc, 1500*time.Millisecond, 3, false)
	for _, s := range []string{"/review-pr 123", "personal", "200 chars", "~50 tok", "/show-skill 2"} {
		if !strings.Contains(live, s) {
			t.Errorf("live card missing %q: %s", s, live)
		}
		if !strings.Contains(static, s) {
			t.Errorf("static card missing %q: %s", s, static)
		}
	}
	// Live card uses a spinner glyph; static uses a bullet.
	if !strings.ContainsAny(live, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Errorf("live card missing spinner glyph: %s", live)
	}
	if !strings.Contains(static, "●") {
		t.Errorf("static card missing bullet: %s", static)
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := map[time.Duration]string{
		50 * time.Millisecond:   "50ms",
		1500 * time.Millisecond: "1.5s",
		125 * time.Second:       "2m5s",
	}
	for in, want := range cases {
		if got := formatElapsed(in); got != want {
			t.Errorf("formatElapsed(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestCommandSuggestions_IncludesShowSkill(t *testing.T) {
	sugg := suggestionsFor(nil)
	var found bool
	for _, s := range sugg {
		if strings.HasPrefix(s.Name, "/show-skill") {
			found = true
		}
	}
	if !found {
		t.Errorf("/show-skill missing from suggestions: %+v", sugg)
	}
}
