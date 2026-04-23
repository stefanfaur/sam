package tui

import (
	"testing"
)

func TestBlockScannerSafeSplit(t *testing.T) {
	tests := []struct {
		name       string
		input      []rune
		wantIdx    int
		wantFenced bool // snapshot does not mutate scanner, so always false here
	}{
		{
			name:    "two paragraphs with blank line",
			input:   []rune("line 1\nline 2\n\nline 3"),
			wantIdx: 15,
		},
		{
			name:    "no blank line",
			input:   []rune("line 1\nline 2"),
			wantIdx: 0,
		},
		{
			name:    "empty tail",
			input:   []rune(""),
			wantIdx: 0,
		},
		{
			name:    "fence open then blank",
			input:   []rune("```\ncode\n```\n\n"),
			wantIdx: 14,
		},
		{
			name:    "unclosed fence blocks split",
			input:   []rune("```\ncode\n\nmore\n"),
			wantIdx: 0,
		},
		{
			name:    "blank after setext heading splits after underline",
			input:   []rune("title\n===\n\nbody"),
			wantIdx: 11,
		},
		{
			name:    "whitespace-only last line without trailing newline does not split",
			input:   []rune("                                    "),
			wantIdx: 0,
		},
		{
			name:    "whitespace-only trailing line after content does not overshoot",
			input:   []rune("para\n    "),
			wantIdx: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &blockScanner{}
			idx := scanner.SafeSplit(tt.input)
			if idx != tt.wantIdx {
				t.Errorf("SafeSplit(%q) = %d, want %d", string(tt.input), idx, tt.wantIdx)
			}
			if scanner.fenceOpen != tt.wantFenced {
				t.Errorf("after SafeSplit, fenceOpen = %v, want %v (must not mutate)", scanner.fenceOpen, tt.wantFenced)
			}
		})
	}
}

func TestBlockScannerAdvance(t *testing.T) {
	tests := []struct {
		name     string
		prefix   []rune
		nextTail []rune
		wantIdx  int
	}{
		{
			name:     "advance past first paragraph",
			prefix:   []rune("para\n\n"),
			nextTail: []rune("next\n\n"),
			wantIdx:  6,
		},
		{
			name:     "advance past fence open, next tail closes fence then blank",
			prefix:   []rune("```\ncode\n"),
			nextTail: []rune("```\n\n"),
			wantIdx:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &blockScanner{}
			scanner.Advance(tt.prefix)
			idx := scanner.SafeSplit(tt.nextTail)
			if idx != tt.wantIdx {
				t.Errorf("SafeSplit after Advance = %d, want %d", idx, tt.wantIdx)
			}
		})
	}
}

func TestBlockScannerAdvancePersistsFenceState(t *testing.T) {
	scanner := &blockScanner{}
	scanner.Advance([]rune("```\ncode\n"))
	if !scanner.fenceOpen {
		t.Fatalf("fenceOpen = false, want true after fence-open prefix")
	}
	scanner.Advance([]rune("```\n"))
	if scanner.fenceOpen {
		t.Fatalf("fenceOpen = true, want false after fence-close prefix")
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"CRLF", "a\r\nb", "a\nb"},
		{"bare CR", "a\rb", "a\nb"},
		{"mixed", "a\r\nb\rc", "a\nb\nc"},
		{"LF only", "a\nb", "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLineEndings(tt.in)
			if got != tt.want {
				t.Errorf("normalizeLineEndings(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripBOM(t *testing.T) {
	tests := []struct {
		name string
		in   []rune
		want []rune
	}{
		{"no BOM", []rune("hello"), []rune("hello")},
		{"with BOM", []rune("\uFEFFhello"), []rune("hello")},
		{"empty", []rune(""), []rune("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBOM(tt.in)
			if string(got) != string(tt.want) {
				t.Errorf("stripBOM(%q) = %q, want %q", string(tt.in), string(got), string(tt.want))
			}
		})
	}
}
