package skills

import (
	"path/filepath"
	"strings"
)

// TrustState is the current trust decision for a project path.
type TrustState int

const (
	TrustPending TrustState = iota
	TrustAllowed
	TrustDenied
)

// TrustList tracks trusted and denied project paths (abs paths only).
type TrustList struct {
	Trusted map[string]bool
	Denied  map[string]bool
}

// NewTrustList builds a TrustList from two maps (either may be nil).
func NewTrustList(trusted, denied map[string]bool) TrustList {
	out := TrustList{
		Trusted: make(map[string]bool, len(trusted)),
		Denied:  make(map[string]bool, len(denied)),
	}
	for k, v := range trusted {
		if v {
			out.Trusted[normalizeTrustPath(k)] = true
		}
	}
	for k, v := range denied {
		if v {
			out.Denied[normalizeTrustPath(k)] = true
		}
	}
	return out
}

// State returns the trust decision for the given project directory.
// If projectDir is empty, returns TrustPending.
func (t TrustList) State(projectDir string) TrustState {
	if projectDir == "" {
		return TrustPending
	}
	abs := normalizeTrustPath(projectDir)
	if t.Denied[abs] {
		return TrustDenied
	}
	if t.Trusted[abs] {
		return TrustAllowed
	}
	return TrustPending
}

// Trust marks a project path as trusted (in-memory only). Mutates t in place.
func (t *TrustList) Trust(projectDir string) {
	p := normalizeTrustPath(projectDir)
	if t.Trusted == nil {
		t.Trusted = map[string]bool{}
	}
	if t.Denied != nil {
		delete(t.Denied, p)
	}
	t.Trusted[p] = true
}

// Deny marks a project path as denied (in-memory only). Mutates t in place.
func (t *TrustList) Deny(projectDir string) {
	p := normalizeTrustPath(projectDir)
	if t.Denied == nil {
		t.Denied = map[string]bool{}
	}
	if t.Trusted != nil {
		delete(t.Trusted, p)
	}
	t.Denied[p] = true
}

// Clear removes the given path from both lists.
func (t *TrustList) Clear(projectDir string) {
	p := normalizeTrustPath(projectDir)
	delete(t.Trusted, p)
	delete(t.Denied, p)
}

// TrustedPaths returns a sorted snapshot of trusted project paths.
func (t TrustList) TrustedPaths() []string {
	out := make([]string, 0, len(t.Trusted))
	for k := range t.Trusted {
		out = append(out, k)
	}
	return out
}

// DeniedPaths returns a sorted snapshot of denied project paths.
func (t TrustList) DeniedPaths() []string {
	out := make([]string, 0, len(t.Denied))
	for k := range t.Denied {
		out = append(out, k)
	}
	return out
}

func normalizeTrustPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return strings.TrimRight(abs, string(filepath.Separator))
}
