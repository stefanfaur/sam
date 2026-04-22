package tui

// scrollback is delivered via tea.Printf from update.go handlers.
// The terminal owns the scrollback buffer; no local history is kept.
