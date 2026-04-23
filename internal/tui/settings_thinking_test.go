package tui

import "testing"

func TestDefaultSettings_ThinkingMode(t *testing.T) {
	s := DefaultSettings()
	if s.Thinking.StreamMode != "full" {
		t.Errorf("default stream mode = %q, want full", s.Thinking.StreamMode)
	}
}

func TestNormalizeThinkingMode(t *testing.T) {
	cases := map[string]string{
		"":         "full",
		"full":     "full",
		"header":   "header",
		"nonsense": "full",
		"HEADER":   "full", // strict match only
	}
	for in, want := range cases {
		s := Settings{Thinking: ThinkingSettings{StreamMode: in}}
		s = normalizeSettings(s)
		if s.Thinking.StreamMode != want {
			t.Errorf("normalize(%q) = %q, want %q", in, s.Thinking.StreamMode, want)
		}
	}
}
