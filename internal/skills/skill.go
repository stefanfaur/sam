package skills

import "time"

// Fingerprint is a stable, path-derived identifier for a skill.
// Format: "sha256:<16 hex>".
type Fingerprint string

// Source labels where a skill was discovered.
type Source string

const (
	SourceProject  Source = "project"
	SourcePersonal Source = "personal"
	SourceOther    Source = "other"
)

// Frontmatter is the YAML frontmatter of a SKILL.md.
type Frontmatter struct {
	Name                   string         `yaml:"name"`
	Description            string         `yaml:"description"`
	ArgumentHint           string         `yaml:"argument-hint"`
	AllowedTools           string         `yaml:"allowed-tools"`
	DisableModelInvocation bool           `yaml:"disable-model-invocation"`
	UserInvocable          *bool          `yaml:"user-invocable"`
	Enabled                *bool          `yaml:"enabled"`
	Unknown                map[string]any `yaml:",inline"`
}

// LoadError captures a per-skill load failure without failing the whole registry.
type LoadError struct {
	Path   string
	Reason string
}

// Skill is a loaded (or load-errored) SKILL.md entry.
type Skill struct {
	Fingerprint Fingerprint
	Path        string
	Root        string
	RootLabel   string
	Source      Source
	Name        string
	Body        string
	FM          Frontmatter
	LoadError   *LoadError
	Shadowed    bool
	Pending     bool

	Enabled        bool
	ModelInvocable bool
	UserInvocable  bool

	LoadedAt time.Time
}

// MaxBodyBytes is the hard cap on rendered skill bodies.
const MaxBodyBytes = 50 * 1024
