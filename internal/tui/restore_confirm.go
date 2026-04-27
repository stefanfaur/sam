package tui

import (
	"fmt"
	"sort"
	"strings"
)

// restoreMode is the user's choice in the restore-confirmation step.
type restoreMode int

const (
	restoreModeUnchosen     restoreMode = iota
	restoreModeConvOnly                 // [c] rewind conversation history only
	restoreModeConvAndCode              // [b] rewind conversation + restore code
)

// restoreConfirm models the modal that follows turn selection: pick mode,
// review the path list (with overlap warnings), confirm.
type restoreConfirm struct {
	turnIndex int
	// candidatePaths are the paths checkpoint.ListPaths returned for the
	// chosen turn — these will be restored when mode == restoreModeConvAndCode.
	candidatePaths []string
	// overlapPaths are paths whose working-tree content differs from the
	// latest snapshot (user-edited after the agent's last turn). Always a
	// subset of candidatePaths. Restoring these would clobber user work.
	overlapPaths []string
	mode         restoreMode
	// includeOverlaps is the user's explicit acknowledgement that overlap
	// paths should still be restored. False by default.
	includeOverlaps bool
	confirmed       bool
}

// newRestoreConfirm constructs the modal. candidates / overlaps may be nil.
func newRestoreConfirm(turn int, candidates, overlaps []string) *restoreConfirm {
	cp := append([]string(nil), candidates...)
	op := append([]string(nil), overlaps...)
	sort.Strings(cp)
	sort.Strings(op)
	return &restoreConfirm{
		turnIndex:      turn,
		candidatePaths: cp,
		overlapPaths:   op,
	}
}

// Mode returns the currently-selected restore mode.
func (r *restoreConfirm) Mode() restoreMode { return r.mode }

// SetMode picks the restore mode (or clears it).
func (r *restoreConfirm) SetMode(m restoreMode) { r.mode = m }

// ToggleIncludeOverlaps flips the overlap-acknowledgement flag.
func (r *restoreConfirm) ToggleIncludeOverlaps() { r.includeOverlaps = !r.includeOverlaps }

// Confirm marks the modal as confirmed (caller drives the actual restore).
// Returns false when the modal cannot yet be confirmed (e.g. no mode chosen).
func (r *restoreConfirm) Confirm() bool {
	if r.mode == restoreModeUnchosen {
		return false
	}
	r.confirmed = true
	return true
}

// IsConfirmed reports whether the user has explicitly confirmed.
func (r *restoreConfirm) IsConfirmed() bool { return r.confirmed }

// PathsToRestore returns the resolved set of paths the caller should pass to
// checkpoint.Restore. Returns nil if mode is conversation-only or unchosen.
// When overlaps exist and includeOverlaps is false, those paths are excluded
// (preserving user edits).
func (r *restoreConfirm) PathsToRestore() []string {
	if r.mode != restoreModeConvAndCode {
		return nil
	}
	if r.includeOverlaps || len(r.overlapPaths) == 0 {
		return append([]string(nil), r.candidatePaths...)
	}
	overlap := make(map[string]bool, len(r.overlapPaths))
	for _, p := range r.overlapPaths {
		overlap[p] = true
	}
	out := make([]string, 0, len(r.candidatePaths))
	for _, p := range r.candidatePaths {
		if !overlap[p] {
			out = append(out, p)
		}
	}
	return out
}

// Render produces the modal body. Width controls truncation of long path
// lines; the caller centers / borders / clears as needed.
func (r *restoreConfirm) Render(t *Theme, width int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rewind to turn %d\n\n", r.turnIndex)

	b.WriteString("Mode: ")
	switch r.mode {
	case restoreModeConvOnly:
		b.WriteString(t.SuggestSelected.Render("[c] conversation only"))
		b.WriteString("    ")
		b.WriteString(t.Suggest.Render("[b] conv + code"))
	case restoreModeConvAndCode:
		b.WriteString(t.Suggest.Render("[c] conversation only"))
		b.WriteString("    ")
		b.WriteString(t.SuggestSelected.Render("[b] conv + code"))
	default:
		b.WriteString(t.Suggest.Render("[c] conversation only    [b] conv + code"))
	}
	b.WriteString("\n\n")

	if r.mode == restoreModeConvAndCode {
		fmt.Fprintf(&b, "%d files will be restored:\n", len(r.candidatePaths))
		shown := r.candidatePaths
		const maxRows = 12
		more := 0
		if len(shown) > maxRows {
			more = len(shown) - maxRows
			shown = shown[:maxRows]
		}
		overlap := make(map[string]bool, len(r.overlapPaths))
		for _, p := range r.overlapPaths {
			overlap[p] = true
		}
		for _, p := range shown {
			line := "  " + oneLine(p, width-4)
			if overlap[p] {
				line = t.Suggest.Render("  ! " + oneLine(p, width-6) + "  (modified by you)")
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		if more > 0 {
			fmt.Fprintf(&b, t.Suggest.Render("  …+%d more\n"), more)
		}
		if len(r.overlapPaths) > 0 {
			b.WriteString("\n")
			marker := "[ ]"
			if r.includeOverlaps {
				marker = "[x]"
			}
			fmt.Fprintf(&b, "%s o  also restore %d files you edited (loses your edits)\n",
				marker, len(r.overlapPaths))
		}
	}

	b.WriteString("\n")
	if r.mode == restoreModeUnchosen {
		b.WriteString(t.Suggest.Render("c/b: choose mode · Esc: cancel"))
	} else {
		b.WriteString(t.Suggest.Render("Enter: confirm · o: toggle overlaps · c/b: switch mode · Esc: cancel"))
	}
	return b.String()
}
