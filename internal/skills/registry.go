package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// exported indirection so tests can stub.
var osStat = os.Stat

func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	return p
}

func classifyRoot(path, cwd, home string) (label string, source Source) {
	cwdAbs, _ := filepath.Abs(cwd)
	projRoot := filepath.Join(cwdAbs, ".sam", "skills")
	if path == projRoot || strings.HasPrefix(path, projRoot+string(os.PathSeparator)) {
		return "project", SourceProject
	}
	if home != "" {
		persRoot := filepath.Join(home, ".sam", "skills")
		if path == persRoot || strings.HasPrefix(path, persRoot+string(os.PathSeparator)) {
			return "personal", SourcePersonal
		}
	}
	return filepath.Base(filepath.Dir(path)), SourceOther
}

// RootSpec tells the registry how to treat a skill root.
type RootSpec struct {
	Path   string
	Label  string
	Source Source
}

// Registry is the in-memory catalogue of loaded skills.
type Registry struct {
	mu        sync.RWMutex
	roots     []RootSpec
	skills    []*Skill
	byName    map[string]*Skill
	byFP      map[Fingerprint]*Skill
	overrides Overrides
	trust     TrustList
	builtins  map[string]bool
	log       *slog.Logger
}

// NewRegistry builds an empty registry. Caller should invoke Load.
func NewRegistry(roots []RootSpec, overrides Overrides, trust TrustList, builtins []string, log *slog.Logger) *Registry {
	bi := make(map[string]bool, len(builtins))
	for _, b := range builtins {
		bi[b] = true
	}
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		roots:     roots,
		overrides: overrides,
		trust:     trust,
		builtins:  bi,
		log:       log,
		byName:    map[string]*Skill{},
		byFP:      map[Fingerprint]*Skill{},
	}
}

// Load scans every configured root and resolves precedence/overrides.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []*Skill
	for _, rs := range r.roots {
		got, err := ScanRoot(rs.Path, rs.Label, rs.Source)
		if err != nil {
			r.log.Warn("skills: scan root failed", "root", rs.Path, "err", err)
			continue
		}
		all = append(all, got...)
	}
	r.applyPrecedence(all)
	r.applyOverridesAndTrust()
	return nil
}

// Reload re-scans roots and rebuilds derived state.
func (r *Registry) Reload() error { return r.Load() }

// SetOverrides swaps overrides and re-applies resolution.
func (r *Registry) SetOverrides(o Overrides) {
	r.mu.Lock()
	r.overrides = o
	r.applyOverridesAndTrust()
	r.mu.Unlock()
}

// Overrides returns a copy of the current overrides snapshot.
func (r *Registry) Overrides() Overrides {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.overrides
}

// Trust returns a copy of the current trust list.
func (r *Registry) Trust() TrustList {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trust
}

// SetTrust swaps trust and re-applies resolution.
func (r *Registry) SetTrust(t TrustList) {
	r.mu.Lock()
	r.trust = t
	r.applyOverridesAndTrust()
	r.mu.Unlock()
}

func (r *Registry) applyPrecedence(all []*Skill) {
	sort.SliceStable(all, func(i, j int) bool {
		if rank(all[i].Source) != rank(all[j].Source) {
			return rank(all[i].Source) < rank(all[j].Source)
		}
		return all[i].Path < all[j].Path
	})
	r.skills = all
	r.byName = map[string]*Skill{}
	r.byFP = map[Fingerprint]*Skill{}
	for _, sk := range all {
		r.byFP[sk.Fingerprint] = sk
		sk.Shadowed = false
		if sk.LoadError != nil {
			continue
		}
		if r.builtins[sk.Name] {
			sk.Shadowed = true
			r.log.Warn("skills: name collides with built-in; only reachable as namespaced", "name", sk.Name, "path", sk.Path)
			continue
		}
		if _, exists := r.byName[sk.Name]; exists {
			sk.Shadowed = true
			continue
		}
		r.byName[sk.Name] = sk
	}
}

// applyOverridesAndTrust sets per-skill Enabled / ModelInvocable / UserInvocable
// / Pending per the §5.3.1 precedence table.
func (r *Registry) applyOverridesAndTrust() {
	for _, sk := range r.skills {
		sk.Pending = false
		sk.Enabled = false
		sk.ModelInvocable = false
		sk.UserInvocable = false
		if sk.LoadError != nil {
			continue
		}

		// Frontmatter hard-overrides.
		fmUserInvocable := true
		if sk.FM.UserInvocable != nil {
			fmUserInvocable = *sk.FM.UserInvocable
		}
		fmAllowModel := !sk.FM.DisableModelInvocation

		// Defaults by source.
		defaultEnabled := true
		defaultModel := true
		if sk.Source == SourceProject {
			defaultModel = false
		}
		// Frontmatter `enabled:` default.
		if sk.FM.Enabled != nil {
			defaultEnabled = *sk.FM.Enabled
		}

		// Override file.
		ov, hasOv := r.overrides.Skills[string(sk.Fingerprint)]
		enabled := defaultEnabled
		model := defaultModel
		manual := fmUserInvocable
		if hasOv {
			if ov.Enabled != nil {
				enabled = *ov.Enabled
			}
			if ov.Auto != nil {
				model = *ov.Auto
			}
			if ov.Manual != nil {
				manual = *ov.Manual
			}
		}

		// Frontmatter hard-overrides win last.
		if !fmAllowModel {
			model = false
		}
		if !fmUserInvocable {
			manual = false
		}

		// Trust / pending gate for project skills.
		if sk.Source == SourceProject {
			switch r.trust.State(sk.Root) {
			case TrustPending:
				sk.Pending = true
				enabled = false
			case TrustDenied:
				enabled = false
			}
		}

		sk.Enabled = enabled
		sk.ModelInvocable = model
		sk.UserInvocable = manual
	}
}

func rank(s Source) int {
	switch s {
	case SourceProject:
		return 0
	case SourcePersonal:
		return 1
	default:
		return 2
	}
}

// Resolve looks up a skill by its slash name ("foo") or namespaced form
// ("personal:foo"). Shadowed skills resolve only via the namespaced form.
// Load-errored skills never resolve.
func (r *Registry) Resolve(input string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if idx := strings.IndexByte(input, ':'); idx >= 0 {
		label, name := input[:idx], input[idx+1:]
		for _, sk := range r.skills {
			if sk.Name == name && sk.RootLabel == label && sk.LoadError == nil {
				return sk, true
			}
		}
		return nil, false
	}
	sk, ok := r.byName[input]
	return sk, ok
}

// List returns a copy of every loaded skill (including shadowed + load-errored).
func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, len(r.skills))
	copy(out, r.skills)
	return out
}

// Get looks up a skill by fingerprint.
func (r *Registry) Get(fp Fingerprint) *Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byFP[fp]
}

// Roots returns the configured roots.
func (r *Registry) Roots() []RootSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RootSpec, len(r.roots))
	copy(out, r.roots)
	return out
}

// SetRoots replaces root specs. Caller should Reload afterwards.
func (r *Registry) SetRoots(roots []RootSpec) {
	r.mu.Lock()
	r.roots = roots
	r.mu.Unlock()
}

// ExpandRoots turns a raw list of roots (may contain "~/..." and relatives)
// into RootSpec list, classifying each as project/personal/other based on cwd
// and home. Missing directories are dropped.
func ExpandRoots(raw []string, cwd, home string) []RootSpec {
	var out []RootSpec
	for _, r := range raw {
		abs := expandHome(r, home)
		label, source := classifyRoot(abs, cwd, home)
		if fi, err := osStat(abs); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, RootSpec{Path: abs, Label: label, Source: source})
	}
	return out
}
