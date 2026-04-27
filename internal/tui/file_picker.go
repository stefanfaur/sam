package tui

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sahilm/fuzzy"
)

// filePicker holds the state for the @file overlay: candidate file list,
// the user's current query, the filtered subset, and the selected row.
type filePicker struct {
	files    []string
	filtered []string
	query    string
	selected int
}

// newFilePicker constructs a picker over the given file list.
func newFilePicker(files []string) *filePicker {
	cp := append([]string(nil), files...)
	return &filePicker{files: cp, filtered: cp}
}

// setQuery updates the filter and resets selection to the top match.
func (fp *filePicker) setQuery(q string) {
	fp.query = q
	fp.filtered = fuzzyFilter(q, fp.files)
	fp.selected = 0
}

// moveSelection adjusts the highlighted row, clamped to the filtered range.
func (fp *filePicker) moveSelection(delta int) {
	n := len(fp.filtered)
	if n == 0 {
		fp.selected = 0
		return
	}
	fp.selected += delta
	if fp.selected < 0 {
		fp.selected = 0
	}
	if fp.selected >= n {
		fp.selected = n - 1
	}
}

// current returns the highlighted file path, or "" if there are no matches.
func (fp *filePicker) current() string {
	if fp.selected < 0 || fp.selected >= len(fp.filtered) {
		return ""
	}
	return fp.filtered[fp.selected]
}

// getFilesForPicker tries `git ls-files` first and falls back to a recursive
// directory walk that skips hidden and common build/vendor directories.
func getFilesForPicker(cwd string) ([]string, error) {
	if files, err := getFilesFromGit(cwd); err == nil && len(files) > 0 {
		return files, nil
	}
	return getFilesRecursive(cwd)
}

func getFilesFromGit(cwd string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		files = append(files, string(p))
	}
	return files, nil
}

var skipDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	"target":       {},
}

func getFilesRecursive(cwd string) ([]string, error) {
	var files []string
	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() {
			if path == cwd {
				return nil
			}
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, skip := skipDirs[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

// renderFilePicker draws the picker overlay above the input box: header line
// showing the live query and a list of up to maxRows filtered candidates.
func (m *Model) renderFilePicker() string {
	if m.picker == nil {
		return ""
	}
	const maxRows = 8
	header := m.theme.Suggest.Render("@" + m.picker.query)
	if len(m.picker.filtered) == 0 {
		return header + "\n" + m.theme.Suggest.Render("  (no matches)")
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	end := len(m.picker.filtered)
	if end > maxRows {
		end = maxRows
	}
	for i := 0; i < end; i++ {
		row := m.picker.filtered[i]
		if i == m.picker.selected {
			b.WriteString(m.theme.SuggestSelected.Render("› " + row))
		} else {
			b.WriteString(m.theme.Suggest.Render("  " + row))
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	if more := len(m.picker.filtered) - end; more > 0 {
		b.WriteString("\n")
		b.WriteString(m.theme.Suggest.Render("  …+" + itoa(more)))
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// fuzzyFilter ranks files by sahilm/fuzzy match score against the query.
// Empty query returns the full list unchanged.
func fuzzyFilter(query string, files []string) []string {
	if query == "" {
		out := make([]string, len(files))
		copy(out, files)
		return out
	}
	matches := fuzzy.Find(query, files)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Str)
	}
	return out
}
