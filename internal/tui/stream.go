package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

type blockScanner struct {
	fenceOpen   bool
	fenceMark   string
	fenceIndent int
	inHTMLBlock bool
}

// SafeSplit returns the largest rune index into tail such that tail[:idx]
// ends on a closed-block boundary. Returns 0 if no safe split exists.
// Does NOT mutate scanner state (scanner is mutated only via Advance).
func (s *blockScanner) SafeSplit(tail []rune) int {
	if len(tail) == 0 {
		return 0
	}

	// Work on a snapshot so SafeSplit stays pure.
	snap := *s

	lines := splitLines(tail)
	committed := 0

	for i, line := range lines {
		lineStart := committed
		committed += len(line) + 1
		if committed > len(tail) {
			committed = len(tail)
		}

		// Fence-open: transitions state, never a split boundary on its own.
		if found, mark, indent := parseFenceOpen(line); found && !snap.fenceOpen && !snap.inHTMLBlock {
			snap.fenceOpen = true
			snap.fenceMark = mark
			snap.fenceIndent = indent
			continue
		}

		// Fence-close: clear state. Split can happen on the next blank line.
		if snap.fenceOpen && isFenceClose(line, snap.fenceMark, snap.fenceIndent) {
			snap.fenceOpen = false
			snap.fenceMark = ""
			snap.fenceIndent = 0
			continue
		}

		// HTML block start/end.
		if !snap.inHTMLBlock && !snap.fenceOpen && hasHTMLBlockStart(line) {
			snap.inHTMLBlock = true
			continue
		}
		if snap.inHTMLBlock && hasHTMLBlockEnd(line) {
			snap.inHTMLBlock = false
			continue
		}

		// Blank line = split candidate, unless followed by setext underline.
		if len(strings.TrimSpace(string(line))) == 0 && !snap.fenceOpen && !snap.inHTMLBlock {
			if i+1 < len(lines) && isSetextUnderline(lines[i+1]) {
				continue
			}
			return lineStart + len(line) + 1
		}
	}

	return 0
}

// Advance updates scanner state to reflect having committed prefix.
func (s *blockScanner) Advance(prefix []rune) {
	lines := splitLines(prefix)
	for _, line := range lines {
		if found, mark, indent := parseFenceOpen(line); found && !s.fenceOpen && !s.inHTMLBlock {
			s.fenceOpen = true
			s.fenceMark = mark
			s.fenceIndent = indent
		} else if s.fenceOpen && isFenceClose(line, s.fenceMark, s.fenceIndent) {
			s.fenceOpen = false
			s.fenceMark = ""
			s.fenceIndent = 0
		} else if !s.inHTMLBlock && !s.fenceOpen && hasHTMLBlockStart(line) {
			s.inHTMLBlock = true
		} else if s.inHTMLBlock && hasHTMLBlockEnd(line) {
			s.inHTMLBlock = false
		}
	}
}

func splitLines(runes []rune) [][]rune {
	var lines [][]rune
	var current []rune
	for _, r := range runes {
		if r == '\n' {
			lines = append(lines, current)
			current = nil
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	return lines
}

func parseFenceOpen(line []rune) (bool, string, int) {
	s := string(line)
	indent := 0
	for _, ch := range s {
		if ch != ' ' {
			break
		}
		indent++
	}
	if indent > 3 {
		return false, "", 0
	}
	rest := s[indent:]
	if strings.HasPrefix(rest, "```") {
		return true, "```", indent
	}
	if strings.HasPrefix(rest, "~~~") {
		return true, "~~~", indent
	}
	return false, "", 0
}

func isFenceClose(line []rune, fenceMark string, fenceIndent int) bool {
	s := string(line)
	indent := 0
	for _, ch := range s {
		if ch != ' ' {
			break
		}
		indent++
	}
	if indent > fenceIndent+3 {
		return false
	}
	rest := s[indent:]
	if !strings.HasPrefix(rest, fenceMark) {
		return false
	}
	tail := rest[len(fenceMark):]
	return len(strings.TrimSpace(tail)) == 0
}

func hasHTMLBlockStart(line []rune) bool {
	s := strings.ToLower(strings.TrimSpace(string(line)))
	for _, start := range []string{"<script", "<pre", "<style", "<textarea"} {
		if strings.HasPrefix(s, start) {
			return true
		}
	}
	return false
}

func hasHTMLBlockEnd(line []rune) bool {
	s := strings.ToLower(string(line))
	for _, end := range []string{"</script>", "</pre>", "</style>", "</textarea>"} {
		if strings.Contains(s, end) {
			return true
		}
	}
	return false
}

func isSetextUnderline(line []rune) bool {
	trimmed := strings.TrimSpace(string(line))
	if len(trimmed) == 0 {
		return false
	}
	for _, ch := range trimmed {
		if ch != '=' && ch != '-' {
			return false
		}
	}
	return true
}

func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func stripBOM(runes []rune) []rune {
	if len(runes) > 0 && runes[0] == '\uFEFF' {
		return runes[1:]
	}
	return runes
}

// safeGlamourRender wraps glamour with a panic guard. On any failure
// returns the raw text unmodified.
func safeGlamourRender(glam *glamour.TermRenderer, text string) (out string) {
	out = text
	defer func() {
		if r := recover(); r != nil {
			out = text
		}
	}()
	if glam == nil {
		return text
	}
	rendered, err := glam.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(rendered, "\n")
}
