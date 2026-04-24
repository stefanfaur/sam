package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// familyFile returns the embedded/on-disk filename for a family name.
// Family names are used verbatim — no case transform (unlike tool names).
func familyFile(family string) string {
	return family + ".md"
}

// EmbeddedFamilyPrompt returns the embedded prompts/<family>.md content.
// Returns "" if the family is unknown. Trailing whitespace trimmed to match
// LoadFamilyPrompt behavior.
func EmbeddedFamilyPrompt(family string) string {
	b, err := defaultsFS.ReadFile("defaults/prompts/" + familyFile(family))
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n\r\t ")
}

// LoadFamilyPrompt reads dir/prompts/<family>.md; returns "" if missing.
// An empty file returns ""; callers must use FamilyPromptExists to
// distinguish a user-wiped file from a missing file.
func LoadFamilyPrompt(dir, family string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "prompts", familyFile(family)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimRight(string(b), "\n\r\t "), nil
}

// FamilyPromptExists reports whether dir/prompts/<family>.md is present on
// disk, regardless of content.
func FamilyPromptExists(dir, family string) bool {
	_, err := os.Stat(filepath.Join(dir, "prompts", familyFile(family)))
	return err == nil
}
