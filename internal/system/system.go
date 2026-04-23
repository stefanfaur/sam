// Package system manages externalized system prompt and tool descriptions
// under ~/.sam/system/. Defaults are embedded at build time and seeded to
// disk on first run; disk files take precedence over embedded fallbacks.
package system

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed defaults/system-prompt.md defaults/tools/*.md
var defaultsFS embed.FS

// DefaultDir returns the on-disk system directory. If $SAM_HOME is set,
// returns $SAM_HOME/system. Otherwise returns ~/.sam/system. Falls back to
// a relative .sam/system if the user home cannot be resolved.
func DefaultDir() string {
	if v := os.Getenv("SAM_HOME"); v != "" {
		return filepath.Join(v, "system")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".sam", "system")
	}
	return filepath.Join(home, ".sam", "system")
}

// EmbeddedPrompt returns the embedded system-prompt.md content.
func EmbeddedPrompt() string {
	b, err := defaultsFS.ReadFile("defaults/system-prompt.md")
	if err != nil {
		return ""
	}
	return string(b)
}

// EmbeddedToolDescription returns the embedded tools/<name>.md content.
// Name is case-insensitive; files on disk are lowercase.
func EmbeddedToolDescription(name string) string {
	b, err := defaultsFS.ReadFile("defaults/tools/" + toolFile(name))
	if err != nil {
		return ""
	}
	// Trim trailing whitespace so embedded text matches the original
	// package-level *Description strings byte-for-byte.
	return strings.TrimRight(string(b), "\n\r\t ")
}

// toolFile returns the embedded/on-disk filename for a tool name.
func toolFile(name string) string {
	return strings.ToLower(name) + ".md"
}
