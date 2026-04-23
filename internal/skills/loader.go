package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ScanRoot walks <root>/<name>/SKILL.md at depth 1 (skill dir depth). Returns
// successfully loaded skills and skills with LoadError set.
func ScanRoot(root, rootLabel string, source Source) ([]*Skill, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skills: %s not a directory", abs)
	}
	// Resolve symlinks in the root itself so the escape check below compares
	// apples to apples with EvalSymlinks(skillDir).
	resolvedRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolvedRoot = abs
	}

	seen := map[uint64]bool{}
	var out []*Skill
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		skillDir := filepath.Join(abs, e.Name())
		resolved, err := filepath.EvalSymlinks(skillDir)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) && resolved != resolvedRoot {
			continue
		}
		st, err := os.Stat(resolved)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			continue
		}
		if ino, ok := inode(st); ok {
			if seen[ino] {
				continue
			}
			seen[ino] = true
		}
		path := filepath.Join(resolved, "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		out = append(out, loadSkillFile(path, abs, rootLabel, source, e.Name()))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func loadSkillFile(path, root, rootLabel string, source Source, dirName string) *Skill {
	sk := &Skill{
		Path:        path,
		Root:        root,
		RootLabel:   rootLabel,
		Source:      source,
		Fingerprint: fingerprint(path),
		LoadedAt:    time.Now(),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		sk.LoadError = &LoadError{Path: path, Reason: "read: " + err.Error()}
		return sk
	}
	if len(data) > MaxBodyBytes {
		sk.LoadError = &LoadError{Path: path, Reason: "body exceeds 50 KB"}
		return sk
	}
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		sk.LoadError = &LoadError{Path: path, Reason: "frontmatter: " + err.Error()}
		return sk
	}
	if err := yaml.Unmarshal(fm, &sk.FM); err != nil {
		sk.LoadError = &LoadError{Path: path, Reason: "yaml: " + err.Error()}
		return sk
	}
	if err := validate(&sk.FM, dirName); err != nil {
		sk.LoadError = &LoadError{Path: path, Reason: err.Error()}
		return sk
	}
	sk.Name = sk.FM.Name
	sk.Body = string(body)
	return sk
}

// splitFrontmatter finds the first "---\n ... \n---\n" fence and returns
// (frontmatterBytes, bodyBytes). Handles both LF and CRLF line endings.
func splitFrontmatter(data []byte) (fm, body []byte, err error) {
	// Normalize CRLF to LF for detection only (not for body content).
	// Require opening fence on the first line.
	if !bytes.HasPrefix(data, []byte("---\n")) && !bytes.HasPrefix(data, []byte("---\r\n")) {
		return nil, nil, fmt.Errorf("missing opening ---")
	}
	openLen := 4
	if bytes.HasPrefix(data, []byte("---\r\n")) {
		openLen = 5
	}
	rest := data[openLen:]
	// search for closing fence preceded by newline
	idxLF := bytes.Index(rest, []byte("\n---\n"))
	idxCR := bytes.Index(rest, []byte("\n---\r\n"))
	idx := idxLF
	closeLen := len("\n---\n")
	if idxCR >= 0 && (idx < 0 || idxCR < idx) {
		idx = idxCR
		closeLen = len("\n---\r\n")
	}
	if idx < 0 {
		return nil, nil, fmt.Errorf("missing closing ---")
	}
	return rest[:idx+1], rest[idx+closeLen:], nil
}

func validate(fm *Frontmatter, dirName string) error {
	if fm.Name == "" {
		return fmt.Errorf("name required")
	}
	if !nameRe.MatchString(fm.Name) {
		return fmt.Errorf("name invalid: %q", fm.Name)
	}
	if fm.Name != dirName {
		return fmt.Errorf("name %q != dir %q", fm.Name, dirName)
	}
	if fm.Description == "" {
		return fmt.Errorf("description required")
	}
	if len(fm.Description) > 1024 {
		return fmt.Errorf("description > 1024 chars")
	}
	return nil
}

func fingerprint(absPath string) Fingerprint {
	h := sha256.Sum256([]byte(absPath))
	return Fingerprint("sha256:" + hex.EncodeToString(h[:8]))
}
